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
	remaining := text

	for i := 0; i < maxVocabChunks && remaining != ""; i++ {
		chunk, _ := TruncateAtParagraph(remaining, maxTokens)
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			break
		}
		chunks = append(chunks, chunk)

		// Advance past what we kept. We need to find where chunk ends in
		// the original remaining text. Because TruncateAtParagraph trims
		// whitespace, we locate the chunk prefix and skip past it.
		idx := strings.Index(remaining, chunk)
		if idx < 0 {
			break
		}
		remaining = strings.TrimSpace(remaining[idx+len(chunk):])
	}

	return chunks
}
