// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The key, asked for on the way into a snapshot.
 *
 * The library listing needed no key. Reading inside one does.
 */
import { useEffect, useState } from "react";

import { SnapshotKeyFields, noKey, type SnapshotKey } from "@/components/SnapshotKeyFields";
import { Muted, Small, useModalControls } from "@/design-system";

export function KeyPrompt({ onOpen }: { onOpen: (key: SnapshotKey) => void | Promise<void> }) {
  const [key, setKey] = useState<SnapshotKey>(noKey());
  const { setConfirm } = useModalControls();

  useEffect(() => {
    setConfirm(() => onOpen(key));
  }, [key, onOpen, setConfirm]);

  return (
    <div>
      <p>
        <Muted>
          <Small>The library listing needed no key. Reading inside one does.</Small>
        </Muted>
      </p>
      <SnapshotKeyFields value={key} onChange={setKey} />
    </div>
  );
}
