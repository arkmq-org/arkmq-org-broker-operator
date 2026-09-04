package version

// ResetDefaults clears cached version/image defaults so they are re-derived
// from the current environment. Needed in tests that modify env vars after
// package init has already cached the values.
func ResetDefaults() {
	defaultVersion = ""
	defaultCompactVersion = ""
	defaultKubeImage = ""
	defaultInitImage = ""
	if IsDevLatestBuild() {
		FullVersionFromCompactVersion["000"] = "0.0.0"
		YacfgProfileVersionFromFullVersion["0.0.0"] = YacfgProfileVersionFromFullVersion[LatestVersion]
		if !IsSupportedActiveMQArtemisVersion("0.0.0") {
			SupportedActiveMQArtemisVersions = append(SupportedActiveMQArtemisVersions, "0.0.0")
		}
	}
}
