# Echo Satellite — High-Level Design and Implementation Plan

## 1. Purpose

Echo Satellite repurposes a rooted Amazon Echo Dot Gen 2 as a self-hosted network voice terminal.

The Echo Dot should provide the hardware-facing capabilities — microphone capture, speaker playback, LEDs, buttons, volume and mute — while a separate gateway performs the changeable parts of the voice-assistant pipeline and integrates with an assistant backend such as Hermes.

The primary architectural goal is **backend independence**: Hermes is the first backend, not part of the device protocol. The same Echo Dot