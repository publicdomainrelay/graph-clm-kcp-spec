package deploy

import "embed"

//go:embed *.sh *.yaml crds/*.yaml crds/gatekeeper/*.yaml crds/derived/*.yaml
var Files embed.FS
