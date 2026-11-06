### Examples

Build an Android app, start an emulator, and run the flows of the `.maestro` folder on it:
```yaml
- android-build:
    inputs:
    - module: app
    - variant: debug
- avd-manager:
    inputs:
    - api_level: 34
    - tag: google_apis
    - abi: x86_64
- maestro-test:
    inputs:
    - app_path: $BITRISE_APK_PATH
- deploy-to-bitrise-io: {}
```

Build an iOS app for the simulator, start a simulator, and run the flows of the `.maestro` folder on it:
```yaml
- xcode-build-for-simulator:
    inputs:
    - project_path: ./ios-sample/ios-sample.xcodeproj
    - scheme: ios-sample
- xcode-start-simulator:
    inputs:
    - destination: platform=iOS Simulator,name=Bitrise iOS default,OS=latest
    - wait_for_boot_timeout: 120
- maestro-test:
    inputs:
    - app_path: $BITRISE_APP_DIR_PATH
- deploy-to-bitrise-io: {}
```

Run only the flows tagged `smoke`, and skip the ones tagged `flaky`:
```yaml
- maestro-test:
    inputs:
    - app_path: $BITRISE_APK_PATH
    - include_tags: smoke
    - exclude_tags: flaky
```

Split the flows across two iOS simulators on the same VM. Start both with **Xcode Start Simulator** before the Step, and set `shard_split` to the number of booted simulators:
```yaml
- xcode-build-for-simulator: {}
- xcode-start-simulator:
    title: Start the first simulator
    inputs:
    - destination: platform=iOS Simulator,name=iPhone 17,OS=latest
    - wait_for_boot_timeout: 120
- xcode-start-simulator:
    title: Start the second simulator
    inputs:
    - destination: platform=iOS Simulator,name=iPhone 17 Pro,OS=latest
    - wait_for_boot_timeout: 120
- maestro-test:
    inputs:
    - app_path: $BITRISE_APP_DIR_PATH
    - shard_split: 2
- deploy-to-bitrise-io: {}
```

Split the flows across parallel Pipeline Workflows, each running on its own VM with one emulator. Tag the flows `shard0` and `shard1`, and every copy of the Workflow runs the flows tagged with its index:
```yaml
pipelines:
  ui-tests:
    workflows:
      maestro-tests:
        parallel: 2

workflows:
  maestro-tests:
    steps:
    - android-build:
        inputs:
        - module: app
        - variant: debug
    - avd-manager:
        inputs:
        - api_level: 34
        - tag: google_apis
        - abi: x86_64
    - maestro-test:
        inputs:
        - app_path: $BITRISE_APK_PATH
        - include_tags: shard$BITRISE_IO_PARALLEL_INDEX
        - test_name: Maestro shard $BITRISE_IO_PARALLEL_INDEX
    - deploy-to-bitrise-io: {}
```
