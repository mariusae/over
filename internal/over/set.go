package over

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mariusae/over/internal/config"
	"github.com/mariusae/over/internal/spec"
)

// Members returns the members of a set, as its repository declares them
// and in the order they expand. The repository is brought up to date
// first, so the answer is the one a resolution would get.
func (o *Over) Members(ctx context.Context, ref spec.Ref) ([]string, error) {
	_, rc, names, err := o.OpenRepoConfig(ctx, ref.Spec)
	if err != nil {
		return nil, err
	}
	members, ok := rc.Set(ref.Spec.Name)
	if !ok {
		return nil, noSet(ref, rc, names)
	}
	return members, nil
}

// EditSet adds members to a set, or, when remove is set, drops them from
// it. The set is created by its first member and is not removed by its
// last; see [Over.DeleteSet] for that.
//
// The change is made in the repository and pushed: a set belongs to the
// repository that declares it, so changing one changes it for every
// machine that names it. EditSet returns the members it changed, which is
// empty when they were all there already, or none of them were.
func (o *Over) EditSet(ctx context.Context, ref spec.Ref, members []string, remove bool, origin Origin) ([]string, error) {
	repo, rc, names, err := o.OpenRepoConfig(ctx, ref.Spec)
	if err != nil {
		return nil, err
	}
	if !remove {
		if err := checkMembers(ref, rc, names, members); err != nil {
			return nil, err
		}
	} else if _, ok := rc.Set(ref.Spec.Name); !ok {
		return nil, noSet(ref, rc, names)
	}

	path := filepath.Join(repo.Dir(), "config.yaml")
	var (
		changed []string
		verb    string
	)
	if remove {
		verb = "drop"
		changed, err = config.RemoveSetMembers(path, ref.Spec.Name, members)
	} else {
		verb = "add"
		changed, err = config.AddSetMembers(path, ref.Spec.Name, members)
	}
	if err != nil {
		return nil, err
	}
	if len(changed) == 0 {
		return nil, nil
	}

	lines := make([]string, 0, len(changed))
	for _, member := range changed {
		lines = append(lines, fmt.Sprintf("%s %s in set %s", verb, member, ref.Spec.Name))
	}
	subject := fmt.Sprintf("config: %s %s in set %s", verb, strings.Join(changed, ", "), ref.Spec.Name)
	if _, err := repo.Commit(ctx, Message(subject, lines, origin)); err != nil {
		return nil, err
	}
	if err := o.PushRepo(ctx, ref.Spec, repo); err != nil {
		return nil, err
	}
	return changed, nil
}

// DeleteSet removes a set from its repository, reporting whether it was
// there, and pushes. The layers it grouped are untouched: a set is a name
// for a group, not the group itself.
func (o *Over) DeleteSet(ctx context.Context, ref spec.Ref, origin Origin) (bool, error) {
	repo, rc, names, err := o.OpenRepoConfig(ctx, ref.Spec)
	if err != nil {
		return false, err
	}
	if _, ok := rc.Set(ref.Spec.Name); !ok {
		return false, noSet(ref, rc, names)
	}
	path := filepath.Join(repo.Dir(), "config.yaml")
	removed, err := config.RemoveSet(path, ref.Spec.Name)
	if err != nil || !removed {
		return false, err
	}
	subject := fmt.Sprintf("config: remove set %s", ref.Spec.Name)
	if _, err := repo.Commit(ctx, Message(subject, nil, origin)); err != nil {
		return false, err
	}
	return true, o.PushRepo(ctx, ref.Spec, repo)
}

// checkMembers rejects a member that cannot resolve, before it is
// committed and pushed where every machine will try it. A member naming
// this repository is checked against it; one naming another repository is
// taken on trust, since confirming it would mean cloning a repository
// just to read a name.
func checkMembers(ref spec.Ref, rc *config.Repo, names []string, members []string) error {
	for _, member := range members {
		m, err := spec.ParseMember(member, ref.Spec)
		if err != nil {
			return err
		}
		if m == ref {
			return fmt.Errorf("%s: a set cannot be a member of itself", member)
		}
		if m.Spec.Repository() != ref.Spec.Repository() {
			continue
		}
		switch {
		case m.Spec.Name == "":
			// The repository as a whole, which is every layer in it.
		case m.Set:
			if _, ok := rc.Set(m.Spec.Name); !ok {
				return noSet(m, rc, names)
			}
		default:
			if !contains(names, m.Spec.Name) {
				return noLayer(m, rc, names)
			}
		}
	}
	return nil
}
