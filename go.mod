module go.astrophena.name/plug

go 1.27

require (
	github.com/a-h/templ v0.3.1070
	github.com/alecthomas/chroma/v2 v2.27.0
	github.com/chzyer/readline v1.5.1
	github.com/coder/websocket v1.8.15
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510
	github.com/klauspost/compress v1.20.3
	github.com/rivo/uniseg v0.4.7
	go.astrophena.name/base v0.26.0
	golang.org/x/net v0.62.0
	golang.org/x/sys v0.49.0
	golang.org/x/term v0.47.0
)

require (
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/a-h/parse v0.0.0-20250122154542-74294addb73e // indirect
	github.com/andybalholm/brotli v1.2.7 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cli/browser v1.3.0 // indirect
	github.com/dlclark/regexp2/v2 v2.12.0 // indirect
	github.com/fatih/color v1.19.0 // indirect
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/go4org/hashtriemap v0.0.0-20260925222741-44e5305f85d9 // indirect
	github.com/lmittmann/tint v1.2.1 // indirect
	github.com/mattn/go-colorable v0.1.16 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/natefinch/atomic v1.0.1 // indirect
	golang.org/x/exp/typeparams v0.0.0-20260820142414-ca536658362e // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	golang.org/x/tools v0.52.0 // indirect
	honnef.co/go/tools v0.8.2 // indirect
)

tool (
	go.astrophena.name/base/devtools/addcopyright
	go.astrophena.name/base/devtools/pre-commit
)

tool go.astrophena.name/plug/cmd/plug

tool honnef.co/go/tools/cmd/staticcheck

tool github.com/a-h/templ/cmd/templ
