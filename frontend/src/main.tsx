import { StrictMode, useEffect, useMemo } from "react";
import { createRoot } from "react-dom/client";

import { AppPresenter } from "./app/app_presenter.js";
import { AppPresenterProvider } from "./app/app_context.js";
import { AppView } from "./app/app_view.js";
import "./index.css";

/**
 * Composition root: the presenter tree is assembled once, outside React, and
 * injected at the top. Nothing below here knows how it is wired.
 */
function Root() {
  const app = useMemo(() => new AppPresenter(), []);

  useEffect(() => {
    void app.init();
    return () => app.dispose();
  }, [app]);

  return (
    <AppPresenterProvider value={app}>
      <AppView />
    </AppPresenterProvider>
  );
}

const el = document.getElementById("root");
if (!el) throw new Error("missing #root element");

createRoot(el).render(
  <StrictMode>
    <Root />
  </StrictMode>,
);
