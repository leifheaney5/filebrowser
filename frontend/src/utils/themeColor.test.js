import { describe, expect, it } from "vitest";
import { themeColorInputValue } from "./themeColor.js";

const presets = [
  { value: "var(--blue)", hex: "#2196f3" },
  { value: "var(--red)", hex: "#f44336" },
];

describe("theme color picker values", () => {
  it("resolves preset tokens to colors accepted by the native picker", () => {
    expect(themeColorInputValue("var(--red)", presets)).toBe("#f44336");
  });

  it("normalizes custom short and full hex colors", () => {
    expect(themeColorInputValue("#AbC", presets)).toBe("#aabbcc");
    expect(themeColorInputValue("#12ABEF", presets)).toBe("#12abef");
  });

  it("uses the default preset for the unset or unsupported stored value", () => {
    expect(themeColorInputValue("", presets)).toBe("#2196f3");
    expect(themeColorInputValue("not-a-color", presets)).toBe("#2196f3");
  });
});
