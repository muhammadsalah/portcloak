// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The passphrase field on the resume dialog.
 *
 * PortCloak does not keep it. Sealing the resumed snapshot with a different one
 * would produce a second bundle nobody could tell apart from the first, so
 * confirming stays disabled until something has been typed.
 */
import { useEffect, useState } from "react";

import { FieldBox, FieldHint, Input, Label, Muted, Small, useModalControls } from "@/design-system";

export function ResumePassphrase({
  note,
  onResume,
}: {
  note: string;
  onResume: (passphrase: string) => void | Promise<void>;
}) {
  const [passphrase, setPassphrase] = useState("");
  const { setConfirmDisabled, setConfirm } = useModalControls();

  useEffect(() => {
    setConfirmDisabled(passphrase === "");
    setConfirm(() => onResume(passphrase));
  }, [passphrase, onResume, setConfirmDisabled, setConfirm]);

  return (
    <div>
      <FieldBox>
        <Label>Passphrase</Label>
        <Input
          type="password"
          placeholder="passphrase"
          value={passphrase}
          onChange={(e) => setPassphrase(e.target.value)}
        />
        <FieldHint>
          PortCloak does not keep it. Sealing the resumed snapshot with a different one would
          produce a second bundle nobody could tell apart from the first.
        </FieldHint>
      </FieldBox>
      {note ? (
        <p>
          <Muted>
            <Small>{note}</Small>
          </Muted>
        </p>
      ) : null}
    </div>
  );
}
