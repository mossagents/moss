package harness

import (
	"fmt"
	"slices"
	"strings"
)

type plannedFeature struct {
	feature Feature
	meta    FeatureMetadata
	index   int
}

type providerStatus int

const (
	providerReady providerStatus = iota
	providerMissing
	providerLaterPhase
)

func (h *Harness) planFeatures(features ...Feature) ([]plannedFeature, error) {
	planned := make([]plannedFeature, 0, len(features))
	for idx, feature := range features {
		if feature == nil {
			continue
		}
		meta, err := metadataForFeature(feature)
		if err != nil {
			return nil, fmt.Errorf("feature %q metadata: %w", feature.Name(), err)
		}
		planned = append(planned, plannedFeature{
			feature: feature,
			meta:    meta,
			index:   idx,
		})
	}
	if len(planned) == 0 {
		return nil, nil
	}

	provided, err := installedFeatureKeys(h.features)
	if err != nil {
		return nil, err
	}

	edges := make([]map[int]struct{}, len(planned))
	indegree := make([]int, len(planned))
	addEdge := func(from, to int) {
		if from == to {
			return
		}
		if edges[from] == nil {
			edges[from] = make(map[int]struct{})
		}
		if _, exists := edges[from][to]; exists {
			return
		}
		edges[from][to] = struct{}{}
		indegree[to]++
	}

	for from := range planned {
		for to := range planned {
			if from == to {
				continue
			}
			if featurePhaseRank(planned[from].meta.Phase) < featurePhaseRank(planned[to].meta.Phase) {
				addEdge(from, to)
			}
		}
	}

	for idx, item := range planned {
		for _, req := range item.meta.Requires {
			if _, ok := provided[req]; ok {
				continue
			}
			providerIdx, status := selectProvider(planned, req, idx)
			switch status {
			case providerReady:
				addEdge(providerIdx, idx)
			case providerLaterPhase:
				return nil, fmt.Errorf("feature %q requires %q, but only a later-phase provider is available", item.feature.Name(), req)
			default:
				return nil, fmt.Errorf("feature %q requires %q, but no provider is registered", item.feature.Name(), req)
			}
		}
	}

	ready := make([]int, 0, len(planned))
	for idx, degree := range indegree {
		if degree == 0 {
			ready = append(ready, idx)
		}
	}
	sortReady(planned, ready)

	ordered := make([]plannedFeature, 0, len(planned))
	for len(ready) > 0 {
		idx := ready[0]
		ready = ready[1:]
		ordered = append(ordered, planned[idx])
		for next := range edges[idx] {
			indegree[next]--
			if indegree[next] == 0 {
				ready = append(ready, next)
				sortReady(planned, ready)
			}
		}
	}

	if len(ordered) != len(planned) {
		names := make([]string, 0, len(planned)-len(ordered))
		for idx, degree := range indegree {
			if degree > 0 {
				names = append(names, planned[idx].feature.Name())
			}
		}
		return nil, fmt.Errorf("feature installation cycle detected: %s", strings.Join(names, ", "))
	}
	return ordered, nil
}

func installedFeatureKeys(features []Feature) (map[string]struct{}, error) {
	keys := make(map[string]struct{})
	for _, feature := range features {
		meta, err := metadataForFeature(feature)
		if err != nil {
			return nil, fmt.Errorf("installed feature %q metadata: %w", feature.Name(), err)
		}
		if meta.Key != "" {
			keys[meta.Key] = struct{}{}
		}
	}
	return keys, nil
}

func metadataForFeature(feature Feature) (FeatureMetadata, error) {
	if feature == nil {
		return FeatureMetadata{Phase: FeaturePhaseConfigure}, nil
	}
	withMeta, ok := feature.(FeatureWithMetadata)
	if !ok {
		return FeatureMetadata{Phase: FeaturePhaseConfigure}, nil
	}
	return normalizeFeatureMetadata(withMeta.Metadata())
}

func normalizeFeatureMetadata(meta FeatureMetadata) (FeatureMetadata, error) {
	meta.Key = strings.TrimSpace(meta.Key)
	switch meta.Phase {
	case "", FeaturePhaseConfigure:
		meta.Phase = FeaturePhaseConfigure
	case FeaturePhaseRuntime, FeaturePhasePostRuntime:
	default:
		return FeatureMetadata{}, fmt.Errorf("unknown phase %q", meta.Phase)
	}
	if len(meta.Requires) == 0 {
		return meta, nil
	}
	normalized := make([]string, 0, len(meta.Requires))
	for _, req := range meta.Requires {
		req = strings.TrimSpace(req)
		if req == "" || slices.Contains(normalized, req) {
			continue
		}
		normalized = append(normalized, req)
	}
	meta.Requires = normalized
	return meta, nil
}

func selectProvider(planned []plannedFeature, key string, dependent int) (int, providerStatus) {
	dependentPhase := featurePhaseRank(planned[dependent].meta.Phase)
	bestIdx := -1
	bestPhase := int(^uint(0) >> 1)
	later := false
	for idx, item := range planned {
		if idx == dependent || item.meta.Key != key {
			continue
		}
		phase := featurePhaseRank(item.meta.Phase)
		if phase <= dependentPhase {
			if bestIdx == -1 || phase < bestPhase || (phase == bestPhase && item.index < planned[bestIdx].index) {
				bestIdx = idx
				bestPhase = phase
			}
			continue
		}
		later = true
	}
	if bestIdx >= 0 {
		return bestIdx, providerReady
	}
	if later {
		return -1, providerLaterPhase
	}
	return -1, providerMissing
}

func featurePhaseRank(phase FeaturePhase) int {
	switch phase {
	case FeaturePhaseRuntime:
		return 1
	case FeaturePhasePostRuntime:
		return 2
	default:
		return 0
	}
}

func sortReady(planned []plannedFeature, ready []int) {
	slices.SortFunc(ready, func(a, b int) int {
		return planned[a].index - planned[b].index
	})
}
