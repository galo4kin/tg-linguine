package articles

import "strings"

const maxVocabChunks = 2

// chunkText splits text into up to maxVocabChunks paragraph-bounded
// pieces, each within maxTokens. Text beyond the chunk limit is dropped.
func chunkText(text string, maxTokens int) []string {
	if text == "" || maxTokens <= 0 {
		return nil
	}

	var chunks []string
	remaining := strings.TrimSpace(text)

	for i := 0; i < maxVocabChunks && remaining != ""; i++ {
		chunk, _ := TruncateAtParagraph(remaining, maxTokens)
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			break
		}
		chunks = append(chunks, chunk)

		// TruncateAtParagraph returns a prefix of remaining (trimmed).
		// Advance past it using HasPrefix for safety.
		if strings.HasPrefix(remaining, chunk) {
			remaining = strings.TrimSpace(remaining[len(chunk):])
		} else {
			break
		}
	}

	return chunks
}
