---
kind: changed
---

**⌘+ and ⌘- keep growing the terminal text** instead of stopping at the largest preset. The size is a pixel count now, stepped 2px a press between 8px and 64px, and Settings ▸ Terminal offers a stepper in place of the five named sizes. `terminal_font_size` in `settings.yaml` is now a number of pixels, and the app converts a saved name such as `large` to its size on first launch.
