package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

type Config struct {
	BotToken   string `json:"bot_token"`
	ChatID     string `json:"chat_id"`
	ChatIDTest string `json:"chat_id_test"`
}
type SongLinkOembedResponse struct {
	Title       string `json:"title"`
	AuthorName  string `json:"author_name"`
	ProviderURL string `json:"provider_url"`
	HTML        string `json:"html"`
	//  thumbnail_url
}

func loadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var config Config
	err = json.NewDecoder(file).Decode(&config)
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func getChatID(config *Config, useTestChannel bool) string {
	if useTestChannel {
		return config.ChatIDTest
	}
	return config.ChatID
}

func parseSongLink(songURL string) (title, artist, youtubeURL string, err error) {
	oembedBaseURL := "https://song.link/oembed"
	params := url.Values{}
	params.Add("url", songURL)
	params.Add("format", "json")

	fullOembedURL := oembedBaseURL + "?" + params.Encode()
	// log.Printf("Attempting to fetch oEmbed data from: %s\n", fullOembedURL)

	resp, httpErr := http.Get(fullOembedURL)
	if httpErr != nil {
		return "", "", "", fmt.Errorf("failed to fetch oembed data from %s: %w", fullOembedURL, httpErr)
	}
	defer resp.Body.Close()

	bodyBytes, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return "", "", "", fmt.Errorf("failed to read oembed response body for %s: %w", songURL, readErr)
	}

	if resp.StatusCode != http.StatusOK {
		// log.Printf("Raw oEmbed error response for %s (status %d): %s\n", songURL, resp.StatusCode, string(bodyBytes))
		return "", "", "", fmt.Errorf("oembed request to %s failed with status %d: %s", fullOembedURL, resp.StatusCode, string(bodyBytes))
	}

	// log.Printf("Raw oEmbed response for %s (status %d): %s\n", songURL, resp.StatusCode, string(bodyBytes))

	var oembedResp SongLinkOembedResponse
	if decodeErr := json.NewDecoder(bytes.NewReader(bodyBytes)).Decode(&oembedResp); decodeErr != nil {
		return "", "", "", fmt.Errorf("failed to decode oembed JSON response for %s: %w. Raw response: %s", songURL, decodeErr, string(bodyBytes))
	}

	rawOembedTitle := strings.TrimSpace(oembedResp.Title)
	rawOembedAuthor := strings.TrimSpace(oembedResp.AuthorName)

	if rawOembedAuthor != "" && !strings.EqualFold(rawOembedAuthor, "youtube") && !strings.EqualFold(rawOembedAuthor, "soundcloud") && !strings.EqualFold(rawOembedAuthor, "spotify") && !strings.Contains(rawOembedAuthor, "Topic") {
		artist = rawOembedAuthor
		if rawOembedTitle != "" {
			if strings.Contains(rawOembedTitle, artist) {
				tempTitle := strings.TrimSpace(strings.ReplaceAll(rawOembedTitle, artist, ""))
				tempTitle = strings.TrimPrefix(tempTitle, " - ")
				tempTitle = strings.TrimPrefix(tempTitle, "- ")
				tempTitle = strings.TrimSuffix(tempTitle, " - ")
				tempTitle = strings.TrimSuffix(tempTitle, " -")
				if tempTitle != "" {
					title = tempTitle
				} else {
					title = rawOembedTitle
				}
			} else {
				title = rawOembedTitle
			}
		}
	} else if rawOembedTitle != "" {

		parts := strings.SplitN(rawOembedTitle, " - ", 2)
		if len(parts) == 2 {
			artist = strings.TrimSpace(parts[0])
			title = strings.TrimSpace(parts[1])
		} else {
			title = rawOembedTitle
		}
	}
	if artist != "" {
		artist = strings.TrimSuffix(artist, " - Topic")
	}

	var extractedYoutubeURL string
	if oembedResp.HTML != "" {
		r := regexp.MustCompile(`src="([^"]*youtube\.com[^"]*(?:embed/|watch\?v=)[a-zA-Z0-9_-]+[^"]*)"`)
		matches := r.FindStringSubmatch(oembedResp.HTML)
		if len(matches) > 1 {
			extractedYoutubeURL = matches[1]
			if strings.Contains(extractedYoutubeURL, "/embed/") {
				extractedYoutubeURL = strings.Replace(extractedYoutubeURL, "/embed/", "/watch?v=", 1)
				if qPos := strings.Index(extractedYoutubeURL, "?"); qPos != -1 {
					urlParts := strings.SplitN(extractedYoutubeURL, "?", 2)
					if len(urlParts) == 2 {
						queryParams := strings.Split(urlParts[1], "&")
						for _, param := range queryParams {
							if strings.HasPrefix(param, "v=") {
								extractedYoutubeURL = urlParts[0] + "?" + param
								break
							}
						}
					}
				}
			}
			// log.Printf("Extracted YouTube URL from oEmbed HTML for %s: %s\n", songURL, extractedYoutubeURL)
		}
	}
	if extractedYoutubeURL == "" && oembedResp.ProviderURL != "" && (strings.Contains(oembedResp.ProviderURL, "youtube.com") || strings.Contains(oembedResp.ProviderURL, "youtu.be")) {
		extractedYoutubeURL = oembedResp.ProviderURL
		// log.Printf("Using ProviderURL as YouTube URL for %s: %s\n", songURL, extractedYoutubeURL)
	}
	youtubeURL = extractedYoutubeURL

	if title == "" && artist == "" && youtubeURL == "" && oembedResp.Title == "" {
		// If nothing was extracted and oEmbed title is empty.
		// This check ensures parseSongLink returns an error only if oEmbed was completely useless,
		// so main can fall back to HTML parsing.
		// But if oEmbed returned anything (e.g., just HTML with a YouTube link), it's not an error.
		// Errors are already handled for failed oEmbed requests or JSON decoding issues.
		// Empty title/artist alone are not considered oEmbed errors.
	}

	log.Printf("oEmbed parse result for %s: Title='%s', Artist='%s', YouTubeURL='%s'\n", songURL, title, artist, youtubeURL)
	return title, artist, youtubeURL, nil
}

func parseSongLinkHTML(songURL string) (rawFullTitle, youtubeURL string, err error) {
	log.Printf("Attempting HTML parsing for: %s\n", songURL)
	resp, httpGetErr := http.Get(songURL)
	if httpGetErr != nil {
		return "", "", fmt.Errorf("html get failed for %s: %w", songURL, httpGetErr)
	}
	defer resp.Body.Close()

	bodyBytes, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return "", "", fmt.Errorf("html failed to read body for %s: %w", songURL, readErr)
	}
	bodyString := string(bodyBytes)

	doc, htmlParseErr := html.Parse(strings.NewReader(bodyString))
	if htmlParseErr != nil {
		log.Printf("Warning: Full HTML parsing with html.Parse failed for %s: %v. Will rely on regex for YouTube URL if possible.\n", songURL, htmlParseErr)
	} else {
		// 1. parsing meta tags (og:title, og:video:url)
		var ogVideoURL string
		var ogTitleFull string

		var traverseMetaTags func(*html.Node)
		traverseMetaTags = func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "meta" {
				var property, contentVal string
				isOgTitle := false
				isOgVideo := false
				for _, attr := range n.Attr {
					if attr.Key == "property" {
						property = attr.Val
						if property == "og:title" {
							isOgTitle = true
						} else if property == "og:video:url" || property == "og:video:secure_url" {
							isOgVideo = true
						}
					}
					if attr.Key == "content" {
						contentVal = attr.Val
					}
				}
				if isOgTitle && contentVal != "" {
					ogTitleFull = contentVal
				}
				if isOgVideo && contentVal != "" && (strings.Contains(contentVal, "youtube.com") || strings.Contains(contentVal, "youtu.be")) {
					ogVideoURL = contentVal
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				traverseMetaTags(c)
			}
		}
		traverseMetaTags(doc)

		if ogVideoURL != "" {
			youtubeURL = ogVideoURL
		}
		if ogTitleFull != "" {
			rawFullTitle = ogTitleFull
		}

		// 2. If og:title gave no info (rawFullTitle is empty), try to find a specific DIV structure
		if rawFullTitle == "" && doc != nil {
			log.Printf("og:title not found or empty for %s. Attempting specific div structure parsing.\n", songURL)

			var divTitle, divArtist string
			var findSpecificDivs func(*html.Node) bool

			findSpecificDivs = func(n *html.Node) bool {
				if n.Type == html.ElementNode && n.Data == "div" {
					var classValue string
					for _, attr := range n.Attr {
						if attr.Key == "class" {
							classValue = attr.Val
							break
						}
					}
					if strings.Contains(classValue, "e12n0mv62") {
						for c := n.FirstChild; c != nil; c = c.NextSibling {
							if c.Type == html.ElementNode && c.Data == "div" {
								var childClassValue string
								for _, attrChild := range c.Attr {
									if attrChild.Key == "class" {
										childClassValue = attrChild.Val
										break
									}
								}
								if divTitle == "" && strings.Contains(childClassValue, "e12n0mv61") {
									divTitle = extractTextFromNode(c)
								}
								if divArtist == "" && strings.Contains(childClassValue, "e12n0mv60") {
									divArtist = extractTextFromNode(c)
								}
							}
						}
						if divTitle != "" && divArtist != "" {
							rawFullTitle = fmt.Sprintf("%s by %s", divTitle, divArtist)
							log.Printf("Found title and artist via specific div structure for %s: %s\n", songURL, rawFullTitle)
							return true
						} else if divTitle != "" {
							rawFullTitle = divTitle
							log.Printf("Found only title via specific div structure for %s: %s\n", songURL, rawFullTitle)
							return true
						}
					}
				}
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if findSpecificDivs(c) {
						return true
					}
				}
				return false
			}
			findSpecificDivs(doc)
		}
	}

	// 3. YouTube URL from regex if not found earlier
	if youtubeURL == "" {
		ytRegex := regexp.MustCompile(`https?://(?:www\.)?(?:youtube\.com/watch\?v=|youtu\.be/)[a-zA-Z0-9_-]{11}`)
		match := ytRegex.FindString(bodyString)
		if match != "" {
			youtubeURL = match
			log.Printf("Found YouTube URL via regex for %s: %s\n", songURL, youtubeURL)
		}
	}

	if rawFullTitle == "" && youtubeURL == "" {
		return "", "", fmt.Errorf("could not extract any useful data from HTML for %s", songURL)
	}

	log.Printf("HTML parsing final result for %s: RawFullTitle='%s', YouTubeURL='%s'\n", songURL, rawFullTitle, youtubeURL)
	return rawFullTitle, youtubeURL, nil
}

func extractTextFromNode(n *html.Node) string {
	if n == nil {
		return ""
	}
	if n.Type == html.TextNode {
		return strings.TrimSpace(n.Data)
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		text := extractTextFromNode(c)
		if text != "" {
			if sb.Len() > 0 {
				sb.WriteString(" ")
			}
			sb.WriteString(text)
		}
	}
	return strings.TrimSpace(sb.String())
}

var markdownV2Replacer = strings.NewReplacer(
	"_", "\\_", "*", "\\*", "[", "\\[", "]", "\\]", "(", "\\(", ")", "\\)",
	"~", "\\~", "`", "\\`", ">", "\\>", "#", "\\#", "+", "\\+", "-", "\\-",
	"=", "\\=", "|", "\\|", "{", "\\{", "}", "\\}", ".", "\\.", "!", "\\!",
)

func sendTextMessage(botToken, chatID, text, parseMode string, disablePreview bool) error {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	params := url.Values{}
	params.Add("chat_id", chatID)
	params.Add("text", text)
	if parseMode != "" {
		params.Add("parse_mode", parseMode)
	}
	if disablePreview {
		params.Add("disable_web_page_preview", "true")
	}

	// Using http.PostForm for simplicity since there are no files
	resp, err := http.PostForm(apiURL, params)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error on sendMessage: %s (status code: %d)", string(respBody), resp.StatusCode)
	}

	return nil
}

func escapeMarkdownV2(text string) string {
	return markdownV2Replacer.Replace(text)
}
func formatDuration(seconds int) string {
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}

func sanitizeFilename(s string) string {
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	s = strings.ReplaceAll(s, ":", "")
	s = strings.ReplaceAll(s, "*", "")
	s = strings.ReplaceAll(s, "?", "")
	s = strings.ReplaceAll(s, "\"", "")
	s = strings.ReplaceAll(s, "<", "")
	s = strings.ReplaceAll(s, ">", "")
	s = strings.ReplaceAll(s, "|", "")
	return s
}

func downloadYouTubeVideo(youtubeURL, fullFilepath, cookiesFile string) error {
	args := []string{
		"-f", "bestvideo+bestaudio/best",
		"--merge-output-format", "mp4",
		"-o", fullFilepath,
	}
	if cookiesFile != "" {
		args = append(args, "--cookies", cookiesFile)
	}
	args = append(args, youtubeURL)
	cmd := exec.Command("yt-dlp", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	go printStream(stdout)
	go printStream(stderr)

	return cmd.Wait()
}

func printStream(reader io.Reader) {
	r := bufio.NewReader(reader)
	var lastLineWasProgress bool // Flag to track if the previous line was a progress line

	for {
		buf := make([]byte, 2048)
		n, err := r.Read(buf)

		if n > 0 {
			text := string(buf[:n])

			cleanText := strings.TrimSuffix(text, "\n")
			cleanText = strings.TrimSuffix(cleanText, "\r")

			if cleanText != "" {

				fmt.Printf("\r\033[K%s", cleanText)
				lastLineWasProgress = !strings.Contains(text, "\n")
			} else if strings.Contains(text, "\n") {

				if lastLineWasProgress {
					fmt.Println()
				}
				lastLineWasProgress = false
			}
		}

		if err != nil {
			if lastLineWasProgress {
				fmt.Println()
			}
			if err != io.EOF {
				fmt.Printf("Stream read error: %v\n", err)
			}
			break
		}
	}
}

func normalizeVideo(inputFile, normalizedFile string) error {
	fmt.Println("Normalizing video (aggressive mode) to fix potential timestamp issues...")

	args := []string{
		"-fflags", "+genpts",
		"-i", inputFile,
		"-vf", "fps=30,setpts=PTS-STARTPTS",
		"-af", "asetpts=PTS-STARTPTS",
		"-ar", "44100",
		"-preset", "ultrafast",
		"-y",
		normalizedFile,
	}

	cmd := exec.Command("ffmpeg", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func processAndCutVideo(inputFile, outputFile string, startTimeSec int, durationSeconds int) error {
	fmt.Println("Processing video with robust filter_complex method (v2)...")

	const fadeDuration = 1.0

	fadeOutStart := float64(durationSeconds) - fadeDuration
	if fadeOutStart < 0 {
		fadeOutStart = 0
	}

	filterComplex := fmt.Sprintf(
		"[0:v]trim=start=%d:duration=%d,setpts=PTS-STARTPTS,crop=ih:ih,scale=400:400[vout];"+
			"[0:a]atrim=start=%d:duration=%d,asetpts=PTS-STARTPTS,afade=t=in:st=0:d=%.2f,afade=t=out:st=%.2f:d=%.2f[aout]",
		startTimeSec, durationSeconds,
		startTimeSec, durationSeconds,
		fadeDuration, fadeOutStart, fadeDuration,
	)

	args := []string{

		//"-ss", strconv.Itoa(startTimeSec),
		"-i", inputFile,

		"-filter_complex", filterComplex,

		"-map", "[vout]",
		"-map", "[aout]",

		"-c:v", "libx264",
		"-profile:v", "baseline",
		"-pix_fmt", "yuv420p",
		"-preset", "medium",
		"-c:a", "aac",
		"-b:a", "128k",
		"-movflags", "+faststart",
		"-y",
		outputFile,
	}

	cmd := exec.Command("ffmpeg", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func sendVideoNote(botToken, chatID, videoPath string, length int, duration int) error {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendVideoNote", botToken)

	file, err := os.Open(videoPath)
	if err != nil {
		return err
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	_ = writer.WriteField("chat_id", chatID)
	_ = writer.WriteField("length", strconv.Itoa(length))
	_ = writer.WriteField("duration", strconv.Itoa(duration))

	part, err := writer.CreateFormFile("video_note", filepath.Base(videoPath))
	if err != nil {
		return err
	}
	_, err = io.Copy(part, file)
	if err != nil {
		return err
	}

	err = writer.Close()
	if err != nil {
		return fmt.Errorf("failed to close multipart writer: %w", err)
	}

	req, err := http.NewRequest("POST", apiURL, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error on sendVideoNote: %s (status code: %d)", string(respBody), resp.StatusCode)
	}

	return nil
}

func main() {
	// 1.
	urlFlag := flag.String("url", "", "URL to a song (e.g., from song.link) (required)")
	startFlag := flag.Int("start", -1, "Start time in seconds (required)")
	durationFlag := flag.Int("duration", -1, "Duration in seconds (required, max 59)")
	songnameFlag := flag.String("songname", "", "Custom song name (optional, requires authorname)")
	authornameFlag := flag.String("authorname", "", "Custom author name (optional, requires songname)")
	cookiesFlag := flag.String("cookies", "youtube_cookies.txt", "Path to a cookies file")
	testFlag := flag.Bool("t", false, "Use the test Telegram channel")
	removeFlag := flag.Bool("r", true, "Remove temporary files after completion (e.g., -r=false to keep)")

	// 2.
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "This tool downloads a song, cuts a fragment, and sends it as a Telegram video note.\n\n")
		flag.PrintDefaults()
	}

	// 3.
	flag.Parse()

	// 4.
	if *urlFlag == "" || *startFlag == -1 || *durationFlag == -1 {
		log.Println("Error: missing required flags: -url, -start, -duration")
		flag.Usage()
		os.Exit(1)
	}

	// 5.
	config, err := loadConfig("config.json")
	if err != nil {
		log.Fatalf("Failed to load config: %v\n", err)
	}

	targetChatID := getChatID(config, *testFlag)
	if *testFlag {
		fmt.Println("🚀 Using TEST channel.")
		if targetChatID == "" {
			log.Fatalln("Error: Test flag -t used, but 'chat_id_test' is not set in config.json")
		}
	}

	urlArg := *urlFlag
	desiredDurationSec := *durationFlag

	if desiredDurationSec < 10 {
		log.Fatalf("Error: Min duration is 10 seconds. Your value: %d\n", desiredDurationSec)
	}
	if desiredDurationSec > 60 {
		fmt.Printf("Warning: Requested duration %d seconds is greater than 60. Clamping to 60 seconds.\n", desiredDurationSec)
		desiredDurationSec = 60
	}

	oembedTitle, oembedArtist, oembedYoutubeURL, oembedErr := parseSongLink(urlArg)
	if oembedErr != nil {
		log.Printf("oEmbed parsing for %s failed or returned incomplete data: %v. Will proceed to HTML fallback if necessary.\n", urlArg, oembedErr)
	}

	var finalArtist, finalTitle, finalYoutubeURL string

	finalArtist = strings.TrimSpace(oembedArtist)
	finalTitle = strings.TrimSpace(oembedTitle)
	finalYoutubeURL = oembedYoutubeURL

	// 2.
	if finalYoutubeURL == "" || (finalTitle == "" && finalArtist == "") {
		log.Printf("Information from oEmbed for %s may be incomplete (Title: '%s', Artist: '%s', YT: '%s'). Attempting HTML parsing fallback.\n", urlArg, finalTitle, finalArtist, finalYoutubeURL)

		// parseSongLinkHTML -> rawFullTitle, htmlYoutubeURL
		rawTextFromHTML, htmlYoutubeURL, htmlErr := parseSongLinkHTML(urlArg)
		if htmlErr != nil {
			log.Printf("HTML parsing fallback for %s also failed or found limited info: %v\n", urlArg, htmlErr)
		} else {

			if finalYoutubeURL == "" && htmlYoutubeURL != "" {
				finalYoutubeURL = htmlYoutubeURL
				log.Printf("Using YouTube URL from HTML fallback for %s: %s\n", urlArg, finalYoutubeURL)
			}

			if (finalTitle == "" && finalArtist == "") && rawTextFromHTML != "" {
				log.Printf("Parsing raw text from HTML fallback: '%s'\n", rawTextFromHTML)
				var htmlParsedTitle, htmlParsedArtist string
				partsBy := strings.SplitN(rawTextFromHTML, " by ", 2)
				if len(partsBy) == 2 {
					htmlParsedTitle = strings.TrimSpace(partsBy[0])
					htmlParsedArtist = strings.TrimSpace(partsBy[1])
				} else {
					partsDash := strings.SplitN(rawTextFromHTML, " - ", 2)
					if len(partsDash) == 2 {
						htmlParsedArtist = strings.TrimSpace(partsDash[0])
						htmlParsedTitle = strings.TrimSpace(partsDash[1])
					} else {
						htmlParsedTitle = rawTextFromHTML
					}
				}
				if htmlParsedArtist != "" {
					htmlParsedArtist = strings.TrimSuffix(htmlParsedArtist, " - Topic")
					htmlParsedArtist = strings.TrimSpace(htmlParsedArtist)
				}

				if finalTitle == "" && htmlParsedTitle != "" {
					finalTitle = htmlParsedTitle
					log.Printf("Using Title from HTML fallback: '%s'\n", finalTitle)
				}
				if finalArtist == "" && htmlParsedArtist != "" {
					finalArtist = htmlParsedArtist
					log.Printf("Using Artist from HTML fallback: '%s'\n", finalArtist)
				}
			}
		}
	}

	downloadURL := finalYoutubeURL
	if downloadURL == "" {
		log.Printf("No YouTube URL found from oEmbed or HTML for %s. Passing original song.link URL to yt-dlp: %s\n", urlArg, urlArg)
		downloadURL = urlArg
	}

	var filenameBaseText string
	var linkDisplayText string

	if *songnameFlag != "" && *authornameFlag != "" {
		songName := *songnameFlag
		authorName := *authornameFlag
		linkDisplayText = fmt.Sprintf("\"%s\" by %s", songName, authorName)
		filenameBaseText = fmt.Sprintf("%s by %s", songName, authorName)
		log.Printf("Using custom display text: '%s'\n", linkDisplayText)
	} else {
		if finalTitle != "" && finalArtist != "" {
			linkDisplayText = fmt.Sprintf("\"%s\" by %s", finalTitle, finalArtist)
			filenameBaseText = fmt.Sprintf("%s by %s", finalTitle, finalArtist)
		} else if finalTitle != "" {
			linkDisplayText = fmt.Sprintf("\"%s\"", finalTitle)
			filenameBaseText = finalTitle
		} else if finalArtist != "" {
			linkDisplayText = fmt.Sprintf("Unknown Song by %s", finalArtist)
			filenameBaseText = finalArtist
		} else {
			log.Printf("No usable title or artist found for %s. Using generic filename and link text.\n", urlArg)
			timestamp := time.Now().Unix()
			filenameBaseText = fmt.Sprintf("track_%d", timestamp)
			linkDisplayText = escapeMarkdownV2(urlArg)
		}
	}

	var textForMarkdownSquareBrackets string
	if linkDisplayText == escapeMarkdownV2(urlArg) && strings.HasPrefix(urlArg, "http") {
		textForMarkdownSquareBrackets = linkDisplayText
	} else {
		textForMarkdownSquareBrackets = escapeMarkdownV2(linkDisplayText)
	}
	messageText := fmt.Sprintf("[%s](%s)", textForMarkdownSquareBrackets, urlArg)

	filenameBase := sanitizeFilename(filenameBaseText)
	if filenameBase == "" || filenameBase == "_" {
		filenameBase = fmt.Sprintf("track_%d_fallback", time.Now().Unix())
	}
	tempDir := "temp"

	fmt.Println("Cleaning up temporary directory before start...")
	err = os.RemoveAll(tempDir)
	if err != nil {
		log.Printf("⚠️ Warning: could not clean up temp directory before start: %v\n", err)
	}
	err = os.MkdirAll(tempDir, os.ModePerm)
	if err != nil {
		log.Fatalf("Failed to create temp directory %s: %v\n", tempDir, err)
	}

	originalDownloadPath := filepath.Join(tempDir, filenameBase+".mp4")
	finalOutputPath := filepath.Join(tempDir, filenameBase+"_cut.mp4")

	fmt.Println("Downloading video from:", downloadURL)

	err = downloadYouTubeVideo(downloadURL, originalDownloadPath, *cookiesFlag)
	if err != nil {
		log.Fatalf("Failed to download video: %v\n", err)
	}

	err = processAndCutVideo(originalDownloadPath, finalOutputPath, *startFlag, desiredDurationSec)
	if err != nil {
		log.Fatalf("Failed to process and cut video: %v\n", err)
	}

	fmt.Printf("\n✅ Done! File: %s\n", finalOutputPath)

	err = sendTextMessage(config.BotToken, targetChatID, messageText, "MarkdownV2", true)
	if err != nil {
		log.Fatalf("❌ Failed to send link message: %v\n", err)
	}
	fmt.Println("✅ Link message sent successfully (without preview)!")

	videoNoteLength := 400
	err = sendVideoNote(config.BotToken, targetChatID, finalOutputPath, videoNoteLength, desiredDurationSec)
	if err != nil {
		log.Fatalf("❌ Failed to send video note: %v\n", err)
	}
	fmt.Println("✅ Video note sent successfully!")

	if *removeFlag {
		fmt.Println("Cleaning up temporary files...")
		err = os.RemoveAll(tempDir)
		if err != nil {
			log.Printf("⚠️ Warning: Failed to remove temporary directory %s: %v\n", tempDir, err)
		} else {
			fmt.Println("✅ Cleanup complete.")
		}
	} else {
		fmt.Printf("✅ Skipping temporary files cleanup. Files are in '%s' directory.\n", tempDir)
	}
}
