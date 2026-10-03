package spec

func SetDefaults(object any) {
	switch typed := object.(type) {
	case *Repository:
		typed.SetDefaults()
	case *SystemContext:
		typed.SetDefaults()
	case *SpecChange:
		typed.SetDefaults()
	}
}
