package game

// Only built-in artwork is accepted; no URLs or uploaded personal photos.
func ValidAvatar(id string) bool {
	switch id {
	case "", "bear", "rabbit", "fox", "deer", "panda", "cat":
		return true
	}
	return false
}
