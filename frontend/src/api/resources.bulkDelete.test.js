import { beforeEach, describe, expect, it, vi } from "vitest";

const { fetchURLMock, renewMock, notifyMock } = vi.hoisted(() => ({
  fetchURLMock: vi.fn(),
  renewMock: vi.fn(),
  notifyMock: { showError: vi.fn() },
}));

vi.mock("@/notify/index.ts", () => ({ notify: notifyMock }));
vi.mock("@/store/index.ts", () => ({
  getters: { isShare: () => false },
  mutations: {},
  state: { sessionId: "test-session" },
}));
vi.mock("@/utils/auth.js", async (importOriginal) => ({
  ...(await importOriginal()),
  renew: renewMock,
}));
vi.mock("@/utils/url.js", () => ({
  getApiPath: () => "/api/resources/bulk",
  getParentDir: (path) => path.slice(0, path.lastIndexOf("/")) || "/",
}));
vi.mock("@/utils/metadataCache.js", () => ({
  invalidateDirMetadataCache: vi.fn(),
}));
vi.mock("./utils.ts", () => ({ fetchURL: fetchURLMock }));

import { bulkDelete } from "./resources.js";

describe("bulkDelete session recovery", () => {
  beforeEach(() => {
    fetchURLMock.mockReset();
    renewMock.mockReset();
    notifyMock.showError.mockClear();
  });

  it("renews an expired session and retries the delete once", async () => {
    const expired = Object.assign(new Error("token is expired or revoked"), {
      status: 401,
    });
    const items = [{ source: "music", path: "/Artist/" }];
    fetchURLMock
      .mockRejectedValueOnce(expired)
      .mockResolvedValueOnce({
        status: 200,
        json: async () => ({ succeeded: items, failed: [] }),
      });

    await expect(bulkDelete(items)).resolves.toEqual({
      succeeded: items,
      failed: [],
    });

    expect(renewMock).toHaveBeenCalledOnce();
    expect(fetchURLMock).toHaveBeenCalledTimes(2);
    expect(fetchURLMock).toHaveBeenNthCalledWith(
      1,
      "/api/resources/bulk",
      expect.objectContaining({ method: "DELETE", body: JSON.stringify(items) }),
    );
    expect(notifyMock.showError).not.toHaveBeenCalled();
  });

  it("does not retry or renew for non-authentication failures", async () => {
    fetchURLMock.mockRejectedValue(Object.assign(new Error("server unavailable"), {
      status: 503,
    }));

    await expect(bulkDelete([{ source: "music", path: "/Artist/" }]))
      .rejects.toThrow("server unavailable");

    expect(renewMock).not.toHaveBeenCalled();
    expect(fetchURLMock).toHaveBeenCalledOnce();
    expect(notifyMock.showError).not.toHaveBeenCalled();
  });
});
