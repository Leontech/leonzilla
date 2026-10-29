module github.com/Leontech/leonzilla

// golang.org/x/* and yaml are resolved from their GitHub mirrors (see the replace lines),
// so the project also builds where proxy.golang.org / golang.org are not reachable.

go 1.24.7

replace golang.org/x/crypto => github.com/golang/crypto v0.41.0

replace golang.org/x/sys => github.com/golang/sys v0.35.0

replace golang.org/x/term => github.com/golang/term v0.34.0

replace golang.org/x/net => github.com/golang/net v0.43.0

replace golang.org/x/text => github.com/golang/text v0.28.0

replace gopkg.in/yaml.v3 => github.com/go-yaml/yaml/v3 v3.0.1

replace golang.org/x/tools => github.com/golang/tools v0.35.0

replace golang.org/x/mod => github.com/golang/mod v0.26.0

replace golang.org/x/sync => github.com/golang/sync v0.16.0

replace gopkg.in/check.v1 => github.com/go-check/check v0.0.0-20201130134442-10cb98267c6c

replace golang.org/x/telemetry => github.com/golang/telemetry v0.0.0-20250710130107-8d8967aff50b

require (
	github.com/jchv/go-webview2 v0.0.0-20260205173254-56598839c808
	github.com/jlaffaye/ftp v0.2.4
	github.com/pkg/sftp v1.13.9
	golang.org/x/crypto v0.41.0
	golang.org/x/sys v0.35.0
	golang.org/x/text v0.28.0
)

require (
	github.com/jchv/go-winloader v0.0.0-20250406163304-c1995be93bd1 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e // indirect
)

replace go.yaml.in/yaml/v3 => github.com/yaml/go-yaml/v3 v3.0.4
