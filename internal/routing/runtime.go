package routing

import (
	"strings"

	"gomodel/internal/providers"
)

func RuntimeInfoByProvider(source RuntimeSnapshotProvider) map[string]CandidateRuntimeInfo {
	if source == nil {
		return nil
	}
	infos := make(map[string]CandidateRuntimeInfo)
	for _, snapshot := range source.ProviderRuntimeSnapshots() {
		name := strings.TrimSpace(snapshot.Name)
		if name == "" {
			continue
		}
		lastError := strings.TrimSpace(snapshot.LastModelFetchError)
		if lastError == "" {
			lastError = strings.TrimSpace(snapshot.LastAvailabilityError)
		}
		infos[name] = CandidateRuntimeInfo{
			Status:    ClassifyProviderRuntime(snapshot),
			LastError: lastError,
		}
	}
	return infos
}

func ClassifyProviderRuntime(snapshot providers.ProviderRuntimeSnapshot) string {
	modelFetchError := strings.TrimSpace(snapshot.LastModelFetchError)
	availabilityError := strings.TrimSpace(snapshot.LastAvailabilityError)
	switch {
	case snapshot.Registered && snapshot.DiscoveredModelCount > 0 && modelFetchError == "":
		if snapshot.UsingCachedModels && snapshot.LastModelFetchSuccessAt == nil {
			return "degraded"
		}
		return "healthy"
	case modelFetchError != "" && snapshot.DiscoveredModelCount > 0:
		return "degraded"
	case modelFetchError != "":
		return "unhealthy"
	case availabilityError != "" && snapshot.DiscoveredModelCount == 0:
		return "unhealthy"
	case snapshot.DiscoveredModelCount > 0:
		return "healthy"
	default:
		return "degraded"
	}
}
