# Maestro Test

[![Step changelog](https://shields.io/github/v/release/bitrise-steplib/bitrise-step-maestro-test?include_prereleases&label=changelog&color=blueviolet)](https://github.com/bitrise-steplib/bitrise-step-maestro-test/releases)

Runs Maestro flows on an Android emulator or iOS simulator running on the Bitrise machine, and exports the results to Test Reports.

<details>
<summary>Description</summary>

Runs your [Maestro](https://maestro.dev) flows locally on the build machine and exports the results.

The Step installs the Maestro CLI, optionally installs your app on the device, runs `maestro test`, and exports:
- the results to the Test Reports page, with each flow's screenshots, `startRecording` videos and logs attached to its test case
- the Maestro test output folder as a zip artifact

Add **Deploy to Bitrise.io** after this Step to upload the results.

#### Device

If a device is already running, for example one started by **AVD Manager** or **Xcode Start Simulator**, the Step runs the flows on it and leaves it running.

If none is running, the Step boots one, and shuts it down when the flows finish:
- Android: an emulator from the newest preinstalled system image (Linux stacks only).
- iOS: the `Bitrise iOS default` simulator of the newest iOS runtime.

To run several Maestro Test Steps on the same device, set **Shut down the device** to `no` on all but the last one. The Step then leaves the device it booted running and exports it, and the next Maestro Test Step runs on it instead of booting another one.

If several devices are running, pass the one to use with `--device` in **Additional maestro test arguments**.

#### Flow variables

Maestro passes every environment variable with the `MAESTRO_` prefix to the flows, so an Env Var or Secret named `MAESTRO_USERNAME` is available as `${MAESTRO_USERNAME}`. To pass a variable under a different name, use `-e` in **Additional maestro test arguments**, for example `-e USERNAME=$TEST_ACCOUNT_USERNAME`.
</details>

## 🧩 Get started

Add this step directly to your workflow in the [Bitrise Workflow Editor](https://docs.bitrise.io/en/bitrise-ci/workflows-and-pipelines/steps/adding-steps-to-a-workflow.html).

You can also run this step directly with [Bitrise CLI](https://github.com/bitrise-io/bitrise).

## ⚙️ Configuration

<details>
<summary>Inputs</summary>

| Key | Description | Flags | Default |
| --- | --- | --- | --- |
| `flow_path` | Maestro flow files or folders containing flow files, one per line. If a folder has a `config.yaml`, Maestro uses it. | required | `.maestro` |
| `app_path` | The .apk or simulator .app to install before the flows run. By default the output of **Android Build** or **Xcode Build for Simulator**, whichever is set. Set your own path to override it, or clear it if the app is already installed. |  | `$BITRISE_APK_PATH $BITRISE_APP_DIR_PATH` |
| `include_tags` | Run only the flows that have at least one of these tags (comma or newline separated). |  |  |
| `exclude_tags` | Skip the flows that have any of these tags (comma or newline separated). |  |  |
| `test_name` | The name the results are shown under on the Test Reports page. | required | `Maestro` |
| `shutdown_device` | Shut down the device the Step booted when the flows finish. A device that was already running is never shut down.  Set it to `no` to leave the booted device running for a later Step, for example another Maestro Test Step. The Step then exports the device as `$BITRISE_EMULATOR_SERIAL` (Android) or `$BITRISE_XCODE_DESTINATION` (iOS). | required | `yes` |
| `maestro_version` | Leave empty to use the Maestro CLI version this Step release was tested with. The Step logs which version it installs.  Set it only when you need a different version before a new Step release ships it. Untested versions can run your flows, but the Step's test output handling is only guaranteed for the tested version.  Use the version number from a [Maestro release](https://github.com/mobile-dev-inc/maestro/releases) tag without the `cli-` prefix. |  |  |
| `additional_args` | Extra arguments appended to the `maestro test` command, for example `-e USERNAME=$TEST_ACCOUNT_USERNAME` or `--no-reinstall-driver`. |  |  |
| `bitrise_test_result_dir` | The per-step folder the Bitrise CLI creates for test results. | required | `$BITRISE_TEST_RESULT_DIR` |
</details>

<details>
<summary>Outputs</summary>

| Environment Variable | Description |
| --- | --- |
| `MAESTRO_JUNIT_PATH` | Path to the JUnit report written by Maestro. |
| `MAESTRO_TEST_OUTPUT_ZIP_PATH` | Path to the zipped Maestro test output folder (screenshots, logs, recordings) in the deploy directory. |
| `BITRISE_EMULATOR_SERIAL` | Serial of the emulator the Step booted and left running. Only set when **Shut down the device** is `no`. |
| `BITRISE_XCODE_DESTINATION` | Destination specifier of the simulator the Step booted and left running. Only set when **Shut down the device** is `no`. |
</details>

## 🙋 Contributing

We welcome [pull requests](https://github.com/bitrise-steplib/bitrise-step-maestro-test/pulls) and [issues](https://github.com/bitrise-steplib/bitrise-step-maestro-test/issues) against this repository.

For pull requests, work on your changes in a forked repository and use the Bitrise CLI to [run step tests locally](https://docs.bitrise.io/en/bitrise-ci/bitrise-cli/running-your-first-local-build-with-the-cli.html).

Learn more about developing steps:

- [Create your own step](https://docs.bitrise.io/en/bitrise-ci/workflows-and-pipelines/developing-your-own-bitrise-step/developing-a-new-step.html)
