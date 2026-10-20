# Maestro Test

[![Step changelog](https://shields.io/github/v/release/bitrise-steplib/bitrise-step-maestro-test?include_prereleases&label=changelog&color=blueviolet)](https://github.com/bitrise-steplib/bitrise-step-maestro-test/releases)

Runs Maestro flows on an Android emulator or iOS simulator and exports the results to Test Reports.

<details>
<summary>Description</summary>

Runs your [Maestro](https://maestro.dev) flows locally on the build machine and exports the results.

The Step installs the Maestro CLI, installs your app on the device, runs `maestro test`, and exports:
- the results to the Test Reports page, with each flow's screenshots, `startRecording` videos and logs attached to its test case
- the Maestro test output folder as a zip artifact

Add **Deploy to Bitrise.io** after this Step to upload the results.

#### Device

The Step runs the flows on a device that is already running. Start one before this Step with **AVD Manager** (Android) or **Xcode Start Simulator** (iOS).

#### Flow variables

Maestro passes every environment variable with the `MAESTRO_` prefix to the flows, so an Env Var or Secret named `MAESTRO_USERNAME` is available as `${MAESTRO_USERNAME}`. To pass a variable under a different name, use `-e` in **Additional maestro test arguments**, for example `-e USERNAME=$TEST_ACCOUNT_USERNAME`.
</details>

## 🧩 Get started

Add this step directly to your workflow in the [Bitrise Workflow Editor](https://docs.bitrise.io/en/bitrise-ci/workflows-and-pipelines/steps/adding-steps-to-a-workflow.html).

You can also run this step directly with [Bitrise CLI](https://github.com/bitrise-io/bitrise).

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

Pass Secrets to the flows under the names the flows use:
```yaml
- maestro-test:
    inputs:
    - app_path: $BITRISE_APK_PATH
    - additional_args: -e USERNAME=$TEST_ACCOUNT_USERNAME -e PASSWORD=$TEST_ACCOUNT_PASSWORD
```


## ⚙️ Configuration

<details>
<summary>Inputs</summary>

| Key | Description | Flags | Default |
| --- | --- | --- | --- |
| `flow_path` | Maestro flow files or folders containing flow files, one per line. If a folder has a `config.yaml`, Maestro uses it. | required | `.maestro` |
| `app_path` | The .apk (Android) or simulator .app (iOS) to install before the flows run. The Step runs the flows on Android or iOS based on this file.  If you build the app with the official Bitrise Steps, use their output: `$BITRISE_APK_PATH` after **Android Build**, or `$BITRISE_APP_DIR_PATH` after **Xcode Build for Simulator**.  An Android App Bundle (.aab) cannot be installed on an emulator. Convert it with **Export Universal APK** before this Step and use its `$BITRISE_APK_PATH`. | required |  |
| `include_tags` | Run only the flows that have at least one of these tags (comma or newline separated). |  |  |
| `exclude_tags` | Skip the flows that have any of these tags (comma or newline separated). |  |  |
| `test_name` | The name the results are shown under on the Test Reports page. | required | `Maestro` |
| `maestro_version` | Leave empty to use the Maestro CLI version this Step release was tested with. The Step logs which version it installs.  Set it only when you need a different version before a new Step release ships it. Untested versions can run your flows, but the Step's test output handling is only guaranteed for the tested version.  Use the version number from a [Maestro release](https://github.com/mobile-dev-inc/maestro/releases) tag without the `cli-` prefix, `2.1.0` or newer. |  |  |
| `additional_args` | Extra arguments appended to the `maestro test` command, for example `-e USERNAME=$TEST_ACCOUNT_USERNAME` or `--no-reinstall-driver`. |  |  |
| `bitrise_test_result_dir` | The per-step folder the Bitrise CLI creates for test results. | required | `$BITRISE_TEST_RESULT_DIR` |
</details>

<details>
<summary>Outputs</summary>

| Environment Variable | Description |
| --- | --- |
| `MAESTRO_JUNIT_PATH` | Path to the JUnit report written by Maestro. |
| `MAESTRO_TEST_OUTPUT_ZIP_PATH` | Path to the zipped Maestro test output folder (screenshots, logs, recordings) in the deploy directory. |
</details>

## 🙋 Contributing

We welcome [pull requests](https://github.com/bitrise-steplib/bitrise-step-maestro-test/pulls) and [issues](https://github.com/bitrise-steplib/bitrise-step-maestro-test/issues) against this repository.

For pull requests, work on your changes in a forked repository and use the Bitrise CLI to [run step tests locally](https://docs.bitrise.io/en/bitrise-ci/bitrise-cli/running-your-first-local-build-with-the-cli.html).

Learn more about developing steps:

- [Create your own step](https://docs.bitrise.io/en/bitrise-ci/workflows-and-pipelines/developing-your-own-bitrise-step/developing-a-new-step.html)
