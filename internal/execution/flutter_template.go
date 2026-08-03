package execution

import (
	"maleolabs.com/anvil/internal/platform"
)

// FlutterBuildPipeline returns the default build pipeline definition for
// Flutter projects, as specified by MVP-002 §3.5 (Pipeline Templates).
//
// The pipeline is named "build" and contains a single "build" stage with
// three tasks, one per Flutter build target (ADR-018 platform metadata):
//   - flutter-web: "flutter build web" — supported on linux, darwin, windows.
//   - flutter-apk: "flutter build apk --release" — supported on linux,
//     darwin, windows.
//   - flutter-ios: "flutter build ios --release" — supported on darwin
//     only (iOS builds require Xcode, ADR-018).
//
// Each task declares its platform support and build target in metadata so
// the pipeline engine can apply platform-aware execution (TS-P7-23: skip
// unsupported targets with a warning) and --target selection (TS-P7-24).
//
// The flutter-apk and flutter-web tasks set an explicit Timeout: Flutter
// builds (especially the first Gradle run, which downloads the toolchain)
// routinely exceed the engine's DefaultTimeout of 5 minutes. Found by E2E
// verification (ST-007-005): an APK cold build took ~320s and was killed by
// the default deadline. 15m covers cold Gradle runs; 10m covers web builds.
//
// This definition is Core-owned template data (like LaravelBuildPipeline);
// it must NOT import internal/flutter (ADR-009 §8.1). The concrete Flutter
// values are template assets, not adapter logic. The canonical platform
// identifiers come from the Core platform package.
//
// Reference: TS-P7-27, MVP-002 §3.5, ADR-018, ADR-009 §8.1
func FlutterBuildPipeline() PipelineDefinition {
	return PipelineDefinition{
		Pipeline: Pipeline{
			Name: "build",
			Stages: []PipelineStage{
				{
					Name: "build",
					Tasks: []Task{
						{
							Name:    "flutter-web",
							Command: "flutter",
							Args:    []string{"build", "web"},
							Timeout: "10m",
							Metadata: &TaskMetadata{
								Platforms: []string{platform.PlatformLinux, platform.PlatformDarwin, platform.PlatformWindows},
								Target:    "web",
							},
						},
						{
							Name:    "flutter-apk",
							Command: "flutter",
							Args:    []string{"build", "apk", "--release"},
							Timeout: "15m",
							Metadata: &TaskMetadata{
								Platforms: []string{platform.PlatformLinux, platform.PlatformDarwin, platform.PlatformWindows},
								Target:    "apk",
							},
						},
						{
							Name:    "flutter-ios",
							Command: "flutter",
							Args:    []string{"build", "ios", "--release"},
							Metadata: &TaskMetadata{
								Platforms: []string{platform.PlatformDarwin},
								Target:    "ios",
							},
						},
					},
				},
			},
		},
	}
}
