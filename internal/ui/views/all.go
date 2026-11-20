package views

// All returns views in model.Tabs order, then the optional tabs turned on.
func All(ctx *Context) []View {
	out := []View{NewOverview(ctx), NewJobs(ctx), NewQueue(ctx), NewNodes(ctx), NewUsage(ctx), NewStorage(ctx)}
	if ctx.Config.SchedulerTab {
		out = append(out, NewScheduler(ctx))
	}
	return out
}
