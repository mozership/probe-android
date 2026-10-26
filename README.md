# probe-android

A [Probe](https://github.com/mozership/probe) action that drives an app on an Android emulator or device: it launches the app, taps and types into it, waits for elements, reads their text and takes screenshots. It works through adb alone: it reads the screen with `uiautomator dump` and acts on it with `input`, so there is nothing to build or install on the device.

A workflow gives the action a name once, under `actions` at its top, and its steps use the name:

```yaml
actions:
  android: github.com/mozership/probe-android@<commit SHA>
```

Probe only takes a full 40-character commit SHA there. The notes of each [release](https://github.com/mozership/probe-android/releases) start with the action and its commit to copy.

## Requirements

| What | How |
|------|-----|
| adb | The Android SDK Platform-Tools, on PATH or in `$ANDROID_HOME/platform-tools` |
| A booted emulator or a device | `emulator -avd <name>`, or a device with USB debugging on. `adb devices` lists it as `device` |
| The app under test | Installed on it: `adb install path/to/app.apk` |

The action does not boot the emulator or install the app. Do that in a step of its own, such as a `shell` step, so that a failed boot is not taken for a failed test.

Turn the animations of the emulator off, as `adb shell settings put global animator_duration_scale 0` and its `window_` and `transition_` siblings do: uiautomator cannot read a screen that is still moving, so the action waits for it.

## Basic Syntax

```yaml
steps:
  - name: Open Network & internet
    uses: android
    with:
      app: com.android.settings
      actions:
        - name: launch
        - name: tap
          selector: text="Network & internet"
        - name: text
          id: airplane
          selector: text="Airplane mode"
    test: res.code == 0 && res.results.airplane == "Airplane mode"
```

## Parameters

These go directly under `with`.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `app` | String | required | The package name of the app the actions drive |
| `actions` | List | required | The actions to take, in order. See [Actions](#actions) |
| `device` | String | the one device | The device, by its serial, such as `emulator-5554`, or its model. When adb lists more than one, it has to be given |
| `timeout` | Duration string or number of seconds | `30s` | Limit for the actions, from the first to the last |

## Actions

Each entry of `actions` names its action in `name`:

| Action | Fields | What it does |
|--------|--------|--------------|
| `launch` | | Launches the app afresh, as its launcher icon does, ending it first if it runs |
| `activate` | | Brings the app to the front as it was, launching it if it is not running |
| `terminate` | | Ends the app |
| `back` | | Presses the back button |
| `tap` | `selector` | Taps the middle of the element |
| `type` | `selector`, `value` | Taps the element and types `value` into it. `input text` types printable ASCII alone |
| `wait` | `selector` | Waits until the element is on the screen |
| `text` | `selector`, `id` | Reads the element's text, or its content description when it has no text, into `res.results` |
| `tree` | `id` | Reads the screen into `res.results`, an element a line, with what a selector can name. Run a step with it under `-v` to see it |
| `screenshot` | `id` | Saves the screen as PNG, and puts its path in `res.filepaths` |

`id` is the key of the result. Without it the action's name is the key.

An action on an element reads the screen until the element is on it, for what is left of `timeout`, and the step fails when it does not come:

```
actions[1] tap: no element matches text="No Such Row" within 5.0s
```

### Selectors

A selector names an element by key=value pairs, separated by spaces. A value with a space goes in double quotes.

| Key | Matches |
|-----|---------|
| `id` | The resource id, whole as `com.example:id/login` or by its name as `login` |
| `text` | The text shown |
| `desc` | The content description, which a screen reader reads, as for an icon |
| `class` | The class, whole as `android.widget.Button` or by its name as `Button` |
| `index` | Which of the matching elements, from 0, in the order `tree` lists them. Without it, the first |

```yaml
selector: login                          # id=login
selector: class=Button text="Sign in"
selector: id=title index=2
```

## Result

| Field | Description |
|-------|-------------|
| `res.code` | `0` when every action succeeded, `1` otherwise |
| `res.results` | What `text` and `tree` read, by key |
| `res.filepaths` | The files `screenshot` saved, by key |
| `res.device` | The serial of the device the actions ran on |

## CI

On GitHub Actions, an Ubuntu runner runs an emulator with KVM. [.github/workflows/test.yml](.github/workflows/test.yml) does this for the e2e tests of this repository, with [android-emulator-runner](https://github.com/ReactiveCircus/android-emulator-runner), which also turns the animations off.
