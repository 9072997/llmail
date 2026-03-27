module github.com/jpennington/llmail

go 1.26.1

require (
	github.com/adrg/xdg v0.5.3
	github.com/anthropics/anthropic-sdk-go v1.27.1
	github.com/blevesearch/bleve/v2 v2.5.7
	github.com/charmbracelet/huh v1.0.0
	github.com/charmbracelet/lipgloss v1.1.0
	github.com/emersion/go-imap/v2 v2.0.0-beta.8
	github.com/emersion/go-message v0.18.2
	github.com/emersion/go-sasl v0.0.0-20241020182733-b788ff22d5a6
	github.com/knights-analytics/hugot v0.6.5
	github.com/mark3labs/mcp-go v0.46.0
	github.com/sashabaranov/go-openai v1.41.2
	github.com/spf13/cobra v1.10.2
	github.com/zalando/go-keyring v0.2.8
	golang.org/x/crypto v0.49.0
	golang.org/x/image v0.38.0
	gopkg.in/yaml.v3 v3.0.1
)

replace github.com/emersion/go-imap/v2 v2.0.0-beta.8 => ./vendored/go-imap-v2

replace github.com/sugarme/tokenizer v0.3.1-0.20251127070628-8dac234bfe56 => github.com/9072997/tokenizer v0.3.1

require (
	github.com/RoaringBitmap/roaring/v2 v2.16.0 // indirect
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/bits-and-blooms/bitset v1.24.4 // indirect
	github.com/blevesearch/bleve_index_api v1.3.6 // indirect
	github.com/blevesearch/geo v0.2.5 // indirect
	github.com/blevesearch/go-faiss v1.0.29 // indirect
	github.com/blevesearch/go-porterstemmer v1.0.3 // indirect
	github.com/blevesearch/gtreap v0.1.1 // indirect
	github.com/blevesearch/mmap-go v1.2.0 // indirect
	github.com/blevesearch/scorch_segment_api/v2 v2.4.4 // indirect
	github.com/blevesearch/segment v0.9.1 // indirect
	github.com/blevesearch/snowballstem v0.9.0 // indirect
	github.com/blevesearch/upsidedown_store_api v1.0.2 // indirect
	github.com/blevesearch/vellum v1.2.0 // indirect
	github.com/blevesearch/zapx/v11 v11.4.3 // indirect
	github.com/blevesearch/zapx/v12 v12.4.3 // indirect
	github.com/blevesearch/zapx/v13 v13.4.3 // indirect
	github.com/blevesearch/zapx/v14 v14.4.3 // indirect
	github.com/blevesearch/zapx/v15 v15.4.3 // indirect
	github.com/blevesearch/zapx/v16 v16.3.2 // indirect
	github.com/catppuccin/go v0.3.0 // indirect
	github.com/charmbracelet/bubbles v1.0.0 // indirect
	github.com/charmbracelet/bubbletea v1.3.10 // indirect
	github.com/charmbracelet/colorprofile v0.4.3 // indirect
	github.com/charmbracelet/x/ansi v0.11.6 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.15 // indirect
	github.com/charmbracelet/x/exp/strings v0.1.0 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/danieljoos/wincred v1.2.3 // indirect
	github.com/daulet/tokenizers v1.26.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/emirpasic/gods v1.18.1 // indirect
	github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f // indirect
	github.com/go-errors/errors v1.5.1 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/gofrs/flock v0.13.0 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/gomlx/exceptions v0.0.3 // indirect
	github.com/gomlx/go-huggingface v0.3.4 // indirect
	github.com/gomlx/go-xla v0.2.2 // indirect
	github.com/gomlx/gomlx v0.27.2 // indirect
	github.com/gomlx/onnx-gomlx v0.4.1 // indirect
	github.com/google/jsonschema-go v0.4.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/knights-analytics/ortgenai v0.1.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-localereader v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.21 // indirect
	github.com/mitchellh/colorstring v0.0.0-20190213212951-d06e56a500db // indirect
	github.com/mitchellh/hashstructure/v2 v2.0.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/mschoch/smat v0.2.0 // indirect
	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/muesli/termenv v0.16.0 // indirect
	github.com/patrickmn/go-cache v2.1.0+incompatible // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/schollz/progressbar/v2 v2.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/sugarme/regexpset v0.0.0-20200920021344-4d4ec8eaf93c // indirect
	github.com/sugarme/tokenizer v0.3.1-0.20251127070628-8dac234bfe56 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.2.0 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/viant/afs v1.30.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	github.com/yalue/onnxruntime_go v1.27.0 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.etcd.io/bbolt v1.4.3 // indirect
	golang.org/x/exp v0.0.0-20260312153236-7ab1446f8b90 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/term v0.41.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	k8s.io/klog/v2 v2.140.0 // indirect
)
