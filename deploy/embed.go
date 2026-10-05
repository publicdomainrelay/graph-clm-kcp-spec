package deploy

import "embed"

//go:embed *.sh *.yaml crds/*.yaml crds/gatekeeper/*.yaml
var Files embed.FS
