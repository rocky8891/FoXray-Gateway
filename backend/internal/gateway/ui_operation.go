package gateway

import "context"

type uiActionContextKey struct{}

func uiActionID(values ...string) string {
	if len(values) > 0 && len(values[0]) == 32 && regexpID(values[0]) {
		return values[0]
	}
	return ""
}

func uiActionFromContext(ctx context.Context) string {
	action, _ := ctx.Value(uiActionContextKey{}).(string)
	return uiActionID(action)
}

func (a *App) submitUI(action, title, group string, fn func(context.Context, *Job) error) (string, error) {
	return a.submitTask(title, group, "manual", fn, action)
}
