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
    - flow_path: .maestro
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
- maestro-test:
    inputs:
    - flow_path: .maestro
- deploy-to-bitrise-io: {}
```

Run only the flows tagged `smoke`, and skip the ones tagged `flaky`:
```yaml
- maestro-test:
    inputs:
    - flow_path: .maestro
    - include_tags: smoke
    - exclude_tags: flaky
```

Pass Secrets to the flows under the names the flows use:
```yaml
- maestro-test:
    inputs:
    - flow_path: .maestro
    - additional_args: -e USERNAME=$TEST_ACCOUNT_USERNAME -e PASSWORD=$TEST_ACCOUNT_PASSWORD
```
