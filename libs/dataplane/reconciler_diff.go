package dataplane

import "context"

type DiffResult struct {
	Remove []Resource
	Add    []Resource
}

func keyDifference[K comparable, V1, V2 any](map1 map[K]V1, map2 map[K]V2) []K {
	var diff []K
	for k := range map1 {
		if _, exists := map2[k]; !exists {
			diff = append(diff, k)
		}
	}
	return diff
}

func (r *Reconciler) Diff(ctx context.Context, curr map[string]Resource, desired map[string]Resource) (DiffResult, error) {
	remove := keyDifference(curr, desired)
	add := keyDifference(desired, curr)

	removeResources := make([]Resource, 0)
	addResources := make([]Resource, 0)

	for _, k := range remove {
		removeItem := curr[k]
		removeResources = append(removeResources, removeItem)
	}

	for _, k := range add {
		addItem := desired[k]
		addResources = append(addResources, addItem)
	}

	return DiffResult{
		Remove: removeResources,
		Add:    addResources,
	}, nil
}
