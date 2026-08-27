/**
 * Teaches styled-components what `props.theme` is.
 *
 * Without this the theme is typed as an empty object and every
 * `${(p) => p.theme.color.primary}` in the app silently becomes `any` — which
 * is the entire reason the tokens were moved into TypeScript in the first
 * place.
 */
import "styled-components";
import type { Theme } from "./theme";

declare module "styled-components" {
  // eslint-disable-next-line @typescript-eslint/no-empty-object-type
  export interface DefaultTheme extends Theme {}
}
