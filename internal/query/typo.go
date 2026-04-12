package query

// Levenshtein returns the optimal string alignment (OSA) distance between
// strings a and b. This extends standard Levenshtein by also counting adjacent
// character transpositions as a single edit, so "imgae"↔"image" has distance 1.
func Levenshtein(a, b string) int {
	ra := []rune(a)
	rb := []rune(b)
	la, lb := len(ra), len(rb)

	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	// dp[i][j] = OSA distance between ra[:i] and rb[:j]
	dp := make([][]int, la+1)
	for i := range dp {
		dp[i] = make([]int, lb+1)
		dp[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		dp[0][j] = j
	}

	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			dp[i][j] = dp[i-1][j-1] + cost // substitute
			if v := dp[i-1][j] + 1; v < dp[i][j] {
				dp[i][j] = v // delete from a
			}
			if v := dp[i][j-1] + 1; v < dp[i][j] {
				dp[i][j] = v // insert into a
			}
			// Transposition (always costs 1 — cost var can be 0 for equal chars)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				if v := dp[i-2][j-2] + 1; v < dp[i][j] {
					dp[i][j] = v
				}
			}
		}
	}
	return dp[la][lb]
}

// knownExtensions is the closed-world list of recognized extensions (without dot).
var knownExtensions = []string{
	"pdf", "doc", "docx", "txt",
	"py", "go", "js", "ts", "jsx", "tsx",
	"jpg", "jpeg", "png", "gif",
	"mp4", "mov", "mp3", "wav",
	"zip", "tar",
}

// CorrectKind returns the canonical kind value for s, tolerating Levenshtein-1 typos.
// Returns ("", false) if no match within distance 1.
func CorrectKind(s string) (canonical string, ok bool) {
	// Direct match first.
	if v, ok := KnownKindValues[s]; ok {
		return v, true
	}
	// Levenshtein-1 over keys.
	for key, val := range KnownKindValues {
		if Levenshtein(s, key) <= 1 {
			return val, true
		}
	}
	return "", false
}

// CorrectExtension returns the canonical extension for s (without dot),
// tolerating Levenshtein-1 typos. Returns ("", false) if no match.
func CorrectExtension(s string) (canonical string, ok bool) {
	// Direct match.
	for _, ext := range knownExtensions {
		if s == ext {
			return ext, true
		}
	}
	// Levenshtein-1.
	for _, ext := range knownExtensions {
		if Levenshtein(s, ext) <= 1 {
			return ext, true
		}
	}
	return "", false
}
