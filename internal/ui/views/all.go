package views

// All returns views in model.Tabs order.
func All(ctx *Context) []View {
	return []View{NewOverview(ctx), NewJobs(ctx), NewQueue(ctx), NewNodes(ctx), NewUsage(ctx), NewStorage(ctx)}
}
