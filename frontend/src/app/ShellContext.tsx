/**
 * App-wide state the shell owns: the current route and the live counters.
 *
 * The counters are what dim the Capture item until there is somewhere to
 * capture from and somewhere to put it, and what puts a number on Activity
 * while a job runs. They are polled here rather than pushed by each screen,
 * because the badge has to be right on a screen that knows nothing about jobs.
 */
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

import { ConfigAPI, JobsAPI } from "../api";
import { useProgress } from "./ProgressContext";
import type { Route } from "./routes";

/** How often the counters are re-read while the app is open. */
const counterPollMs = 5000;

interface Shell {
  route: Route;
  navigate: (route: Route) => void;
  /**
   * Counts navigations, including a navigation to the screen already showing.
   *
   * Several screens finish an action by going back to where they started —
   * deleting a snapshot returns to the library, saving an environment returns
   * to the environment list — and expect to find it re-read. Without something
   * that changes on every navigate, React would see the same route, keep the
   * mounted screen, and show the list exactly as it was before the delete.
   */
  nonce: number;
  environments: number;
  storages: number;
  activeJobs: number;
  hasSnapshots: boolean;
  setHasSnapshots: (has: boolean) => void;
}

const ShellContext = createContext<Shell | null>(null);

export function ShellProvider({ children }: { children: ReactNode }) {
  const [{ route, nonce }, setLocation] = useState<{ route: Route; nonce: number }>({
    route: { name: "library" },
    nonce: 0,
  });
  const [environments, setEnvironments] = useState(0);
  const [storages, setStorages] = useState(0);
  const [activeJobs, setActiveJobs] = useState(0);
  const [hasSnapshots, setHasSnapshots] = useState(true);

  const refreshCounters = useCallback(async () => {
    try {
      const config = await ConfigAPI.load();
      setEnvironments(config.environments.length);
      setStorages(config.storage.length);
    } catch {
      // A configuration that will not load is reported by the library view,
      // which has room to say which line is wrong.
    }
    try {
      const activity = await JobsAPI.list();
      setActiveJobs(activity.running + activity.interrupted);
    } catch {
      setActiveJobs(0);
    }
  }, []);

  useEffect(() => {
    void refreshCounters();
    // The counters are cheap and the alternative is a stale badge, which is
    // worse than a small poll.
    const poll = window.setInterval(() => void refreshCounters(), counterPollMs);
    return () => window.clearInterval(poll);
  }, [refreshCounters]);

  // A job reaching a terminal state changes the Activity badge.
  useProgress((event) => {
    if (event.kind === "jobState") void refreshCounters();
  });

  const navigate = useCallback((next: Route) => {
    setLocation((previous) => ({ route: next, nonce: previous.nonce + 1 }));
  }, []);

  const value = useMemo<Shell>(
    () => ({
      route,
      navigate,
      nonce,
      environments,
      storages,
      activeJobs,
      hasSnapshots,
      setHasSnapshots,
    }),
    [route, navigate, nonce, environments, storages, activeJobs, hasSnapshots],
  );

  return <ShellContext.Provider value={value}>{children}</ShellContext.Provider>;
}

export function useShell(): Shell {
  const shell = useContext(ShellContext);
  if (!shell) throw new Error("useShell was called outside ShellProvider");
  return shell;
}

/** The one thing most screens need from the shell. */
export function useNavigate(): (route: Route) => void {
  return useShell().navigate;
}
