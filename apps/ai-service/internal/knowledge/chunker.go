package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

type ChunkItem struct {
	Heading     string
	Content     string
	ContentHash string
}

var (
	titleRegex   = regexp.MustCompile(`^#\s+`)
	headingRegex = regexp.MustCompile(`^#{2,3}\s+`)
	stripHRegex  = regexp.MustCompile(`^#+\s*`)
)

func sha256Hash(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])
}

// ChunkMarkdown splits a markdown document by headings (H2, H3) and applies word-based overlap.
func ChunkMarkdown(markdown string, chunkSize, chunkOverlap int, chunkerVersion string) ([]ChunkItem, error) {
	if chunkSize <= 0 {
		return nil, fmt.Errorf("chunk_size must be > 0")
	}
	if chunkOverlap < 0 || chunkOverlap >= chunkSize {
		return nil, fmt.Errorf("chunk_overlap must satisfy 0 <= chunk_overlap < chunk_size")
	}

	sections := splitByHeadings(markdown)
	var chunks []ChunkItem

	for _, sec := range sections {
		body := strings.TrimSpace(sec.body)
		if body == "" {
			continue
		}
		parts := splitWithOverlap(body, chunkSize, chunkOverlap)
		for _, part := range parts {
			chunks = append(chunks, ChunkItem{
				Heading:     strings.TrimSpace(sec.heading),
				Content:     part,
				ContentHash: sha256Hash(part),
			})
		}
	}
	return chunks, nil
}

type headingSection struct {
	heading string
	body    string
}

func splitByHeadings(markdown string) []headingSection {
	raw := strings.ReplaceAll(markdown, "\r\n", "\n")
	lines := strings.Split(raw, "\n")

	var sections []headingSection
	var currentHeading string
	var currentBody []string
	titleSeen := false

	for _, line := range lines {
		if titleRegex.MatchString(line) && !titleSeen {
			titleSeen = true
			continue
		}
		if headingRegex.MatchString(line) {
			if len(currentBody) > 0 {
				sections = append(sections, headingSection{
					heading: currentHeading,
					body:    strings.Join(currentBody, "\n"),
				})
			}
			currentHeading = stripHRegex.ReplaceAllString(line, "")
			currentBody = []string{}
		} else {
			currentBody = append(currentBody, line)
		}
	}

	if len(currentBody) > 0 {
		sections = append(sections, headingSection{
			heading: currentHeading,
			body:    strings.Join(currentBody, "\n"),
		})
	}
	return sections
}

func wordsOf(text string) []string {
	return strings.Fields(text)
}

func splitWithOverlap(body string, chunkSize, chunkOverlap int) []string {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}

	rawParas := strings.Split(body, "\n\n")
	var paras []string
	for _, p := range rawParas {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			paras = append(paras, trimmed)
		}
	}

	// Pre-slice oversized paragraphs
	var units []string
	step := chunkSize - chunkOverlap
	if step <= 0 {
		step = 1
	}

	for _, p := range paras {
		w := wordsOf(p)
		if len(w) <= chunkSize {
			units = append(units, p)
		} else {
			for i := 0; i < len(w); i += step {
				end := i + chunkSize
				if end > len(w) {
					end = len(w)
				}
				units = append(units, strings.Join(w[i:end], " "))
				if end == len(w) {
					break
				}
			}
		}
	}

	var parts []string
	var curWords []string

	for _, unit := range units {
		uWords := wordsOf(unit)
		if len(curWords)+len(uWords) <= chunkSize {
			curWords = append(curWords, uWords...)
		} else {
			if len(curWords) > 0 {
				parts = append(parts, strings.Join(curWords, " "))
				tailStart := len(curWords) - chunkOverlap
				if tailStart < 0 {
					tailStart = 0
				}
				curWords = append([]string{}, curWords[tailStart:]...)
			}
			curWords = append(curWords, uWords...)
			for len(curWords) > chunkSize {
				parts = append(parts, strings.Join(curWords[:chunkSize], " "))
				tailStart := chunkSize - chunkOverlap
				if tailStart < 0 {
					tailStart = 0
				}
				curWords = append([]string{}, curWords[tailStart:]...)
			}
		}
	}

	if len(curWords) > 0 {
		parts = append(parts, strings.Join(curWords, " "))
	}
	return parts
}
