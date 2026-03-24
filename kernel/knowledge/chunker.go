package knowledge

// ChunkText 将文本按固定大小分块，带重叠区域。
//
// chunkSize 为每个块的最大字符数，overlap 为块间重叠字符数。
// 始终在空白处断开（如果可能），避免切割词语。
func ChunkText(text string, chunkSize, overlap int) []string {
	if chunkSize <= 0 {
		chunkSize = 1000
	}
	if overlap < 0 || overlap >= chunkSize {
		overlap = chunkSize / 5
	}

	runes := []rune(text)
	if len(runes) <= chunkSize {
		return []string{text}
	}

	var chunks []string
	start := 0
	for start < len(runes) {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}

		// 尝试在空白处断开
		if end < len(runes) {
			for i := end; i > start+chunkSize/2; i-- {
				if runes[i] == ' ' || runes[i] == '\n' || runes[i] == '\t' {
					end = i
					break
				}
			}
		}

		chunks = append(chunks, string(runes[start:end]))

		// 已到文本末尾，无需继续
		if end >= len(runes) {
			break
		}

		// 下一块起始位置（考虑重叠）
		start = end - overlap
		if start <= 0 && end > 0 {
			start = end
		}
	}

	return chunks
}
