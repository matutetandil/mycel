package parser

import "sync"

// The text each file was parsed from, by path.
//
// A template written as an HCL string — `key = "product:${input.id}"` — is
// kept as its source text, since HCL cannot evaluate `input` when the file is
// read. That text used to be re-read from disk by range, which silently gave
// nothing for a source that is not on disk: the editor's unsaved buffer, or a
// test's in-memory document. A cache block with both `key` and `key_from`
// then parsed as if it had only `key_from`. The text is remembered here as
// it is parsed and consulted before the disk.
var sourceTexts sync.Map // path → []byte

func rememberSource(path string, src []byte) {
	sourceTexts.Store(path, src)
}

func sourceOf(path string) ([]byte, bool) {
	src, ok := sourceTexts.Load(path)
	if !ok {
		return nil, false
	}
	return src.([]byte), true
}
