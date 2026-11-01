package files

import (
	"regexp"
	"strings"
)

// A Kind is what a file is for, by its name: it decides how Bield serves the
// file inline, and the web app which preview to show.
type Kind string

// The kinds of file.
const (
	KindImage   Kind = "image"
	KindVideo   Kind = "video"
	KindAudio   Kind = "audio"
	KindPDF     Kind = "pdf"
	KindText    Kind = "text"
	KindDocx    Kind = "docx"
	KindSheet   Kind = "sheet"
	KindArchive Kind = "archive"
	KindOther   Kind = "other"
)

var kindExtensions = map[Kind]string{
	KindImage:   "png jpg jpeg jfif gif webp avif bmp ico svg apng",
	KindVideo:   "mp4 m4v webm ogv mov mkv",
	KindAudio:   "mp3 wav ogg oga opus m4a aac flac weba",
	KindPDF:     "pdf",
	KindDocx:    "docx docm dotx",
	KindSheet:   "xlsx xlsm xlsb xls ods csv tsv numbers",
	KindArchive: "zip tar gz tgz bz2 xz zst 7z rar deb rpm dmg iso jar apk",
	KindText: `txt text md markdown mdx rst adoc org tex log out err
		json jsonc json5 jsonl ndjson geojson yaml yml toml ini cfg conf cnf config properties env editorconfig
		xml xsd xsl plist html htm xhtml css scss sass less styl
		js mjs cjs jsx ts mts cts tsx vue svelte astro
		py pyi rb go rs java kt kts scala groovy gradle swift m mm c h cc cpp cxx hpp hh cs fs vb
		php pl pm lua r jl dart ex exs erl hrl clj cljs elm hs ml mli nim zig v sol
		sh bash zsh fish ksh ps1 psm1 bat cmd awk sed
		sql graphql gql proto prisma tf tfvars hcl nix cmake mk make dockerfile containerfile
		service timer socket mount target path network netdev link rules desktop
		diff patch srt vtt ass nfo lock sum mod pem crt cer csr pub asc gpg-key
		gitignore gitattributes gitmodules dockerignore npmrc nvmrc htaccess bashrc zshrc profile vimrc tmux
		csproj sln pbxproj xcconfig entitlements rc list repo sources pref manifest webmanifest map`,
}

var kindByExt = func() map[string]Kind {
	m := map[string]Kind{}
	for kind, exts := range kindExtensions {
		for e := range strings.FieldsSeq(exts) {
			m[e] = kind
		}
	}
	return m
}()

// textNames are files without an extension that are almost always text.
var textNames = func() map[string]bool {
	m := map[string]bool{}
	for n := range strings.FieldsSeq(`
		dockerfile containerfile makefile gnumakefile rakefile gemfile procfile vagrantfile jenkinsfile caddyfile justfile brewfile
		readme license copying authors contributors changelog changes news todo notice install
		hosts hostname fstab crontab passwd group shells resolv.conf sudoers environment motd issue timezone machine-id
		authorized_keys known_hosts config id_rsa.pub id_ed25519.pub`) {
		m[n] = true
	}
	return m
}()

var textPatterns = regexp.MustCompile(`^(readme|license|changelog)\b|\.(log|conf)\.\d+$`)

// extOf returns a name's extension in lower case, without the dot. A name
// that is only a dot and a word, such as ".bashrc", is all extension.
func extOf(name string) string {
	name = strings.ToLower(name)
	dot := strings.LastIndexByte(name, '.')
	switch {
	case dot > 0:
		return name[dot+1:]
	case dot == 0:
		return name[1:]
	default:
		return ""
	}
}

// KindOf returns what a file called name is for.
func KindOf(name string) Kind {
	lower := strings.ToLower(name)
	if k, ok := kindByExt[extOf(lower)]; ok {
		return k
	}
	if textNames[lower] || textPatterns.MatchString(lower) {
		return KindText
	}
	return KindOther
}

var mimeTypes = map[string]string{
	"png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg", "jfif": "image/jpeg", "gif": "image/gif", "webp": "image/webp",
	"avif": "image/avif", "bmp": "image/bmp", "ico": "image/x-icon", "svg": "image/svg+xml", "apng": "image/apng",
	"mp4": "video/mp4", "m4v": "video/mp4", "webm": "video/webm", "ogv": "video/ogg", "mov": "video/quicktime", "mkv": "video/x-matroska",
	"mp3": "audio/mpeg", "wav": "audio/wav", "ogg": "audio/ogg", "oga": "audio/ogg", "opus": "audio/ogg", "m4a": "audio/mp4",
	"aac": "audio/aac", "flac": "audio/flac", "weba": "audio/webm",
	"pdf": "application/pdf",
}

// InlineType returns the content type to show a file called name in the
// browser with, or "" if it must be downloaded. Only media, PDFs and text,
// which is shown as plain text, are ever shown inline: HTML runs script, so
// it never is.
func InlineType(name string) string {
	switch kind, ext := KindOf(name), extOf(name); {
	case kind == KindText || ext == "csv" || ext == "tsv":
		return "text/plain; charset=utf-8"
	case kind == KindImage || kind == KindVideo || kind == KindAudio || kind == KindPDF:
		return mimeTypes[ext]
	default:
		return ""
	}
}
