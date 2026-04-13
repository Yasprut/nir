package policy

import "sort"

type stepWrapper struct {
	step     Step
	priority int
}

// MergeSteps объединяет плоский список шагов в финальный маршрут.
//
// На вход подаётся уже собранный список шагов (из steps + conditional_steps),
// привязанный к политикам через policiesForMerge.
//
// Логика:
//  1. override - если есть, берутся ТОЛЬКО шаги из override-политик.
//  2. restrict - удаляет шаги целиком по имени.
//  3. Одноимённые шаги объединяются: approvers складываются, mode = ALL если хоть один ALL.
//  4. Глобальная дедупликация approvers между шагами.
func MergeSteps(policies []Policy) []Step {
	// Сначала собираем все шаги с привязкой к типу и приоритету
	type taggedStep struct {
		step     Step
		pType    string
		priority int
	}

	var allTagged []taggedStep
	hasOverride := false

	for _, p := range policies {
		if p.Type == "override" {
			hasOverride = true
		}
		for _, s := range p.Steps {
			allTagged = append(allTagged, taggedStep{step: s, pType: p.Type, priority: p.Priority})
		}
	}

	// Override: только шаги из override-политик
	if hasOverride {
		var filtered []taggedStep
		for _, ts := range allTagged {
			if ts.pType == "override" {
				filtered = append(filtered, ts)
			}
		}
		allTagged = filtered
	}

	// Сортируем по приоритету DESC
	sort.SliceStable(allTagged, func(i, j int) bool {
		return allTagged[i].priority > allTagged[j].priority
	})

	stepMap := make(map[string]*stepWrapper)
	seenApprovers := make(map[string]bool)
	var restrictNames []string

	for _, ts := range allTagged {
		step := ts.step

		if ts.pType == "restrict" {
			restrictNames = append(restrictNames, step.Name)
			continue
		}

		if step.Order == 0 {
			step.Order = 1000
		}

		key := step.Name
		step.Approvers.Static = filterSeen(step.Approvers.Static, seenApprovers)
		step.Approvers.Dynamic = filterDynamicSeen(step.Approvers.Dynamic, seenApprovers)

		existing, exists := stepMap[key]

		if !exists {
			if len(step.Approvers.Static)+len(step.Approvers.Dynamic) == 0 {
				continue
			}
			stepMap[key] = &stepWrapper{step: step, priority: ts.priority}
		} else {
			existing.step.Approvers.Static = append(existing.step.Approvers.Static, step.Approvers.Static...)
			existing.step.Approvers.Dynamic = append(existing.step.Approvers.Dynamic, step.Approvers.Dynamic...)
			if step.Mode == "ALL" || existing.step.Mode == "ALL" {
				existing.step.Mode = "ALL"
			}
		}

		for _, s := range step.Approvers.Static {
			seenApprovers[s] = true
		}
		for _, d := range step.Approvers.Dynamic {
			seenApprovers[d.Role] = true
		}
	}

	for _, name := range restrictNames {
		delete(stepMap, name)
	}

	var merged []Step
	for _, sw := range stepMap {
		merged = append(merged, sw.step)
	}

	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].Order == merged[j].Order {
			return merged[i].Mode == "ALL" && merged[j].Mode != "ALL"
		}
		return merged[i].Order < merged[j].Order
	})

	return merged
}

func filterSeen(slice []string, seen map[string]bool) []string {
	var result []string
	for _, s := range slice {
		if !seen[s] {
			result = append(result, s)
		}
	}
	return result
}

func filterDynamicSeen(slice []DynamicApprover, seen map[string]bool) []DynamicApprover {
	var result []DynamicApprover
	for _, d := range slice {
		if !seen[d.Role] {
			result = append(result, d)
		}
	}
	return result
}
