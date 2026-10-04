package deploy

import "embed"

//go:embed *.sh *.yaml crds/*.yaml
var Files embed.FS
