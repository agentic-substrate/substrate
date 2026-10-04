package artifacts

import (
	"errors"
	"unicode/utf8"
)

func readBundle(checkout string, src Source) (string, Source, error) {
	invalid := errors.New("source bundle unavailable: declare at most 32 distinct regular dependency files in the same commit, with UTF-8 text and at most one MiB combined")
	if len(src.Files) > 32 {
		return "", Source{}, invalid
	}
	content, main, err := readSource(checkout, src)
	if err != nil {
		return "", Source{}, err
	}
	if !utf8.ValidString(content) {
		return "", Source{}, invalid
	}
	total := len(content)
	files := make([]SourceFile, 0, len(src.Files))
	seen := map[string]bool{src.Path: true}
	for _, file := range src.Files {
		if seen[file.Path] || file.Content != "" {
			return "", Source{}, invalid
		}
		seen[file.Path] = true
		bytes, source, err := readSource(checkout, Source{Commit: src.Commit, Path: file.Path, Blob: file.Blob})
		if err != nil {
			return "", Source{}, err
		}
		total += len(bytes)
		if total > 1024*1024 || !utf8.ValidString(bytes) {
			return "", Source{}, invalid
		}
		files = append(files, SourceFile{Path: source.Path, Blob: source.Blob, Content: bytes})
	}
	main.Files = files
	return content, main, nil
}
