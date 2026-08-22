package collector

import (
	"context"
	"sort"

	"github.com/billyham07/harness-observer/internal/model"
	"github.com/billyham07/harness-observer/internal/source"
)

type Collector struct{ Sources []source.Source }

func (c Collector) Collect(ctx context.Context, w source.Window) ([]model.Turn, error) {
	var all []model.Turn
	for _, s := range c.Sources {
		rows, err := s.Collect(ctx, w)
		if err != nil {
			return all, err
		}
		all = append(all, rows...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Timestamp.Before(all[j].Timestamp) })
	return all, nil
}
