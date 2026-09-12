package over

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mariusae/over/internal/config"
	"github.com/mariusae/over/internal/spec"
)

// A Created reports a layer that Init made.
type Created struct {
	// Spec identifies the new layer.
	Spec spec.Spec

	// Root is the root the layer declares in its repository, before
	// environment variables are expanded.
	Root string
}

// Init creates layers: it declares each one in its repository's
// config.yaml, then commits and pushes. A layer needs nothing else to
// exist -- its directory appears the first time a file is written to it
// -- so this is all it takes to go from an empty repository to a layer
// that "over add" will accept.
//
// The repository itself must exist, but it may be empty: a repository
// just created and never pushed to has no commits and no branch, and
// Init makes the first commit in it.
//
// An empty root means the default, $HOME.
func (o *Over) Init(ctx context.Context, args []string, root string, origin Origin) ([]Created, error) {
	if root == "" {
		root = "$HOME"
	}
	if _, err := config.ExpandRoot(root); err != nil {
		return nil, err
	}

	// Layers sharing a repository are created in one commit.
	var order []string
	byRepo := map[string][]spec.Spec{}
	for _, arg := range args {
		s, err := spec.Parse(arg)
		if err != nil {
			return nil, err
		}
		if s.Name == "" {
			return nil, fmt.Errorf("%s: name the layer to create, as owner/repo:layer", arg)
		}
		key := s.Repository().String()
		if _, ok := byRepo[key]; !ok {
			order = append(order, key)
		}
		byRepo[key] = append(byRepo[key], s)
	}

	var created []Created
	for _, key := range order {
		specs := byRepo[key]
		repo, err := o.Repo(ctx, specs[0])
		if err != nil {
			return nil, err
		}
		if err := repo.Update(ctx); err != nil {
			return nil, err
		}
		path := filepath.Join(repo.Dir(), "config.yaml")
		rc, err := config.LoadRepo(path)
		if err != nil {
			return nil, err
		}
		names, err := rc.LayerNames(repo.Dir())
		if err != nil {
			return nil, err
		}

		var added []string
		for _, s := range specs {
			if _, ok := rc.Set(s.Name); ok {
				return nil, fmt.Errorf("%s: %s is a set in %s, not a layer", s, s.Name, s.Repository())
			}
			if contains(names, s.Name) {
				return nil, fmt.Errorf("%s: the layer already exists; add it with 'over add %s'", s, s)
			}
			if err := config.AddLayer(path, s.Name, root); err != nil {
				return nil, err
			}
			added = append(added, s.Name)
			created = append(created, Created{Spec: s, Root: root})
		}

		noun := "layer"
		if len(added) > 1 {
			noun = "layers"
		}
		subject := fmt.Sprintf("config: add %s %s", noun, strings.Join(added, ", "))
		lines := make([]string, 0, len(added))
		for _, name := range added {
			lines = append(lines, fmt.Sprintf("create %s (root %s)", name, root))
		}
		if _, err := repo.Commit(ctx, Message(subject, lines, origin)); err != nil {
			return nil, err
		}
		if err := repo.Push(ctx); err != nil {
			return nil, err
		}
	}
	return created, nil
}
