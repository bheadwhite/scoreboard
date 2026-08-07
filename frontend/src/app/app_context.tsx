import { createContext, useContext } from "react";

import type { AppPresenter } from "./app_presenter.js";

const AppPresenterContext = createContext<AppPresenter | null>(null);

export const AppPresenterProvider = AppPresenterContext.Provider;

/**
 * The root presenter, injected at the top of the tree. Only the root travels by
 * context; every child presenter is passed as a prop, so each view states its
 * own dependency.
 */
export function useAppPresenter(): AppPresenter {
  const presenter = useContext(AppPresenterContext);
  if (!presenter) {
    throw new Error("useAppPresenter must be used within an AppPresenterProvider");
  }
  return presenter;
}
