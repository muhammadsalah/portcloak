// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * How the four kinds of storage are named, and what their secret actually is.
 *
 * The credential label is the one that matters. "Credential" over an S3 field
 * is what makes an operator paste an access key without its secret, and find
 * out at the end of a capture rather than at the start of one.
 */
import { describe, expect, it } from "vitest";

import { credentialLabel, kindLabel, kinds } from "./kinds";

describe("the kinds", () => {
  it("are the four the engine supports, in the order the editor offers them", () => {
    expect(kinds.map((kind) => kind.value)).toEqual(["disk", "ssh", "s3", "azure"]);
  });

  it("are named the way the rest of the app names them", () => {
    expect(kindLabel("disk")).toBe("Disk");
    expect(kindLabel("ssh")).toBe("SSH");
    expect(kindLabel("s3")).toBe("S3");
    expect(kindLabel("azure")).toBe("Azure Blob");
  });

  it("show an unknown kind rather than hiding it", () => {
    // A config written by a newer version should read oddly, not read blank.
    expect(kindLabel("gcs")).toBe("gcs");
    expect(kindLabel("")).toBe("");
  });
});

describe("the credential label", () => {
  it("says what an S3 secret is, including the shape it is pasted in", () => {
    expect(credentialLabel("s3")).toBe("Access key and secret (key:secret)");
  });

  it("names all three things Azure will accept", () => {
    expect(credentialLabel("azure")).toBe("Connection string, account key or SAS");
  });

  it("stays generic where the secret genuinely is just one", () => {
    expect(credentialLabel("disk")).toBe("Credential");
    expect(credentialLabel("ssh")).toBe("Credential");
  });
});
