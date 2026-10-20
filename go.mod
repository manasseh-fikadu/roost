module github.com/manasseh-fikadu/roost

go 1.27.0

// The web app's npm packages, which the go command would otherwise walk for ./...
ignore ./web/node_modules

tool (
	github.com/gzuidhof/tygo
	golang.org/x/vuln/cmd/govulncheck
)

require (
	github.com/fatih/structtag v1.2.0 // indirect
	github.com/gzuidhof/tygo v0.2.21 // indirect
	github.com/inconshreveable/mousetrap v1.0.0 // indirect
	github.com/spf13/cobra v1.3.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/telemetry v0.0.0-20261010193011-f71dc92da45e // indirect
	golang.org/x/tools v0.52.0 // indirect
	golang.org/x/vuln v1.9.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)
