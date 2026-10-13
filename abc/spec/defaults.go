package spec

func SetDefaults(object any) {
	switch typed := object.(type) {
	case *Repository:
		typed.SetDefaults()
	case *SystemContext:
		typed.SetDefaults()
	case *SpecChange:
		typed.SetDefaults()
	case *Plan:
		typed.SetDefaults()
	case *Finding:
		typed.SetDefaults()
	case *AgentRun:
		typed.SetDefaults()
	}
}
