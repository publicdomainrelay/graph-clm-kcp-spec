package specd

import (
	"context"

	packabc "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/contextpack"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	packimpl "github.com/publicdomainrelay/graph-clm-kcp-spec/impl/contextpack"
)

// contextPack renders the pack every model call of this controller gets: the
// repository card, the task card with the verify and acceptance commands, the
// owner context, the ranked neighbors and the open findings, proposals and plan
// items. It is empty when no graph is configured, and a pack that cannot be
// read never fails the change it was for.
func (c *Controller) contextPack(ctx context.Context, namespace string, repository *spec.Repository, contextName string) string {
	if c.opts.GraphReader == nil {
		return ""
	}
	pack, err := packimpl.ForContext(ctx, contextName, packimpl.Options{
		Reader:     c.opts.GraphReader,
		Cluster:    c.client,
		Namespace:  namespace,
		Repository: repository,
		Budget:     c.opts.Budget,
	})
	if err != nil {
		c.log.Warn("the context pack is not built", "systemcontext", contextName, "err", err)
		return ""
	}
	text, _ := packabc.Render(pack, c.opts.Budget)
	return text
}
