import { useSignalValue } from "@tcn/state/react";

import { useAppPresenter } from "./app_context.js";

/**
 * Offers the new build when one has shipped.
 *
 * It asks rather than reloading on its own. Somebody could be halfway through
 * naming teams or typing out why they are objecting to a claim, and a page that
 * swaps itself out to be helpful would throw that away — which is a poor trade
 * for a version of a scoreboard that will still be there in a minute.
 *
 * Sits in normal flow above the sticky top bar rather than floating over it, so
 * it never covers the title or the back button and needs no coordination with
 * the layout underneath. It scrolls away like anything else; the check that put
 * it there does not reset, so it is waiting again at the top of the page.
 */
export function UpdateBanner() {
  const app = useAppPresenter();
  const available = useSignalValue(app.updateAvailableBroadcast);

  if (!available) return null;

  return (
    <div className="update" role="status">
      <span className="update__text">✨ A newer version is ready.</span>
      <button className="btn btn--sm btn--primary" onClick={app.applyUpdate}>
        Refresh
      </button>
    </div>
  );
}
