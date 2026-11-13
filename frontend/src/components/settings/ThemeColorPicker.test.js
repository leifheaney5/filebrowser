import { afterEach, describe, expect, it } from "vitest";
import { createApp, defineComponent, h, nextTick, ref } from "vue";
import ThemeColorPicker from "./ThemeColorPicker.vue";
import UserProfilePreferences from "./UserProfilePreferences.vue";

const presets = [
  { label: "Blue", value: "var(--blue)", hex: "#2196f3" },
  { label: "Red", value: "var(--red)", hex: "#f44336" },
];

function mountPicker({ initialColor = "var(--blue)", disabled = false } = {}) {
  const selectedColor = ref(initialColor);
  const root = document.createElement("div");
  document.body.append(root);
  const app = createApp(defineComponent({
    setup() {
      return () => h(ThemeColorPicker, {
        modelValue: selectedColor.value,
        presets,
        disabled,
        label: "Theme color",
        "onUpdate:modelValue": (value) => { selectedColor.value = value; },
      });
    },
  }));
  app.mount(root);
  return { app, root, selectedColor };
}

describe("ThemeColorPicker", () => {
  let mounted;

  afterEach(() => {
    mounted?.app.unmount();
    mounted?.root.remove();
    mounted = undefined;
  });

  it("offers the native color control alongside preset colors", () => {
    mounted = mountPicker();
    expect(mounted.root.querySelectorAll(".button-group button")).toHaveLength(2);
    expect(mounted.root.querySelector('input[type="color"]').value).toBe("#2196f3");
  });

  it("updates the existing color preference when a custom color is chosen", async () => {
    mounted = mountPicker();
    const picker = mounted.root.querySelector('input[type="color"]');
    picker.value = "#654321";
    picker.dispatchEvent(new Event("change", { bubbles: true }));
    await nextTick();
    expect(mounted.selectedColor.value).toBe("#654321");
    expect(mounted.root.querySelector(".custom-color-choice span").textContent).toBe("#654321");
  });

  it("keeps preset selection working after adding the custom picker", async () => {
    mounted = mountPicker();
    mounted.root.querySelectorAll(".button-group button")[1].click();
    await nextTick();
    expect(mounted.selectedColor.value).toBe("var(--red)");
    expect(mounted.root.querySelector('input[type="color"]').value).toBe("#f44336");
  });

  it("disables both custom and preset controls when settings are locked", () => {
    mounted = mountPicker({ disabled: true });
    expect(mounted.root.querySelector('input[type="color"]').disabled).toBe(true);
    expect([...mounted.root.querySelectorAll(".button-group button")].every((button) => button.disabled)).toBe(true);
    const picker = mounted.root.querySelector('input[type="color"]');
    picker.value = "#654321";
    picker.dispatchEvent(new Event("change", { bubbles: true }));
    expect(mounted.selectedColor.value).toBe("var(--blue)");
  });

  it("routes custom selections through the existing profile-save event", () => {
    const emitted = [];
    const context = {
      sections: { ui: { darkMode: true } },
      $emit: (...args) => emitted.push(args),
      emitSectionChange: (...args) => emitted.push(["change", ...args]),
    };

    UserProfilePreferences.methods.setColor.call(context, "#654321");

    expect(context.sections).toEqual({ ui: { darkMode: true, themeColor: "#654321" } });
    expect(emitted).toEqual([
      ["theme-color", "#654321"],
      ["change", "ui", "themeColor"],
    ]);
  });
});
