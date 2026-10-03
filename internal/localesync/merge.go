package localesync

func mergeLocale(shipped, baseline, user *node) *node {
	if !shipped.isObject() || !user.isObject() {
		return mergeLeaf(shipped, baseline, user)
	}

	merged := emptyObject()
	merged.isInline = shipped.isInline
	for _, key := range shipped.keys {
		shippedChild := shipped.children[key]
		baselineChild := baseline.child(key)
		userChild := user.child(key)
		if userChild == nil {
			if baselineChild == nil {
				merged.add(key, shippedChild, shipped.spaced[key])
			}
			continue
		}
		merged.add(key, mergeLocale(shippedChild, baselineChild, userChild), shipped.spaced[key])
	}

	for _, key := range user.keys {
		if shipped.child(key) != nil {
			continue
		}
		userChild := user.children[key]
		if equalNodes(userChild, baseline.child(key)) {
			continue
		}
		merged.add(key, userChild, user.spaced[key])
	}
	return merged
}

func mergeLeaf(shipped, baseline, user *node) *node {
	if shipped.isObject() != user.isObject() {
		return user
	}
	if baseline != nil && !baseline.isObject() && equalLeaves(user.raw, baseline.raw) {
		return shipped
	}
	return user
}
