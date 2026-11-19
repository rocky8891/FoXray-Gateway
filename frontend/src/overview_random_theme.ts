import { useCallback, useEffect, useRef, useState } from "react";
import { record, type Dict } from "./api";
import { randomHomeServiceTheme, serviceThemeSnapshot } from "./service_themes";
import { themedAppearance } from "./theme_appearance";

type Trial = {
  baseline: Dict;
  display: Dict;
  version: number;
  deadline: number | null;
};
type Committed = { display: Dict; expected: Dict };
export type HomeRandomTheme = {
  values: Dict;
  pending: boolean;
  saving: boolean;
  failed: boolean;
  deadline: number | null;
  shuffle: (source: Dict) => void;
  restore: () => void;
  cancel: () => Promise<void>;
  register: (source: Dict) => void;
  saveNow: (extra: Dict) => Promise<boolean>;
};
function same(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (!a || !b || typeof a !== "object" || typeof b !== "object") return false;
  const x = a as Dict,
    y = b as Dict;
  return (
    Object.keys(x).length === Object.keys(y).length &&
    Object.keys(x).every((key) => same(x[key], y[key]))
  );
}
function display(values: Dict): Dict {
  const next = { ...themedAppearance(values) };
  delete next.__appearance_resolved;
  return next;
}
export function useOverviewRandomTheme(
  presentation: unknown,
  save: (values: Dict) => Promise<boolean>,
): HomeRandomTheme {
  const [trial, setTrial] = useState<Trial | null>(null),
    [committed, setCommitted] = useState<Committed | null>(null);
  const [saving, setSaving] = useState(false),
    [failed, setFailed] = useState(false);
  const latest = useRef({ presentation: record(presentation), save });
  latest.current = { presentation: record(presentation), save };
  const active = useRef<Trial | null>(null),
    visible = useRef<Dict | null>(null),
    generation = useRef(0),
    alive = useRef(true),
    wrote = useRef(false);
  const serial = useRef<Promise<void>>(Promise.resolve());
  const update = (next: Trial | null) => {
    active.current = next;
    setTrial(next);
  };
  const enqueue = useCallback(
    (version: number, patch: Dict, done: () => void) => {
      const request = serial.current.then(async () => {
        if (!alive.current || version !== generation.current) return false;
        setSaving(true);
        wrote.current = true;
        try {
          const ok = await latest.current.save(patch);
          if (alive.current && version === generation.current) {
            if (ok) {
              done();
              setFailed(false);
            } else setFailed(true);
          }
          return ok;
        } catch {
          if (alive.current && version === generation.current) setFailed(true);
          return false;
        } finally {
          if (alive.current) setSaving(false);
        }
      });
      serial.current = request.then(() => undefined);
      return request;
    },
    [],
  );
  const finish = (patch: Dict, shown: Dict) => {
    setCommitted({ display: shown, expected: patch });
    update(null);
    wrote.current = false;
  };
  useEffect(() => {
    if (!trial || trial.deadline === null) return;
    const version = trial.version;
    const timer = setTimeout(
      () => {
        const current = active.current;
        if (
          !current ||
          current.version !== version ||
          version !== generation.current
        )
          return;
        const shown = visible.current || {
          ...latest.current.presentation,
          ...current.display,
        };
        const patch = serviceThemeSnapshot(display(shown));
        void enqueue(version, patch, () => finish(patch, display(patch)));
      },
      Math.max(0, trial.deadline - Date.now()),
    );
    return () => clearTimeout(timer);
  }, [trial?.version, trial?.deadline, enqueue]);
  useEffect(() => {
    if (
      committed &&
      Object.entries(committed.expected).every(([key, value]) =>
        same(record(presentation)[key], value),
      )
    )
      setCommitted(null);
  }, [presentation, committed]);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      generation.current++;
    };
  }, []);
  const cancel = useCallback(() => {
    generation.current++;
    active.current = null;
    setTrial(null);
    setCommitted(null);
    setFailed(false);
    wrote.current = false;
    return serial.current;
  }, []);
  const register = useCallback((source: Dict) => {
    visible.current = source;
  }, []);
  const shuffle = (source: Dict) => {
    const resolved = display(source);
    const theme = randomHomeServiceTheme(resolved);
    const baseline = active.current?.baseline || serviceThemeSnapshot(resolved);
    if (!active.current) wrote.current = false;
    const shown = display(
      serviceThemeSnapshot({ ...resolved, ...display(theme.values) }),
    );
    visible.current = { ...source, ...shown };
    setFailed(false);
    update({
      baseline,
      display: shown,
      version: ++generation.current,
      deadline: Date.now() + 60000,
    });
  };
  const restore = () => {
    const current = active.current;
    if (!current) return;
    const version = ++generation.current,
      patch = current.baseline,
      shown = display(patch);
    const needsWrite =
      wrote.current ||
      !same(serviceThemeSnapshot(display(latest.current.presentation)), patch);
    update(null);
    setFailed(false);
    visible.current = { ...latest.current.presentation, ...shown };
    setCommitted({ display: shown, expected: patch });
    if (needsWrite) {
      update({ baseline: patch, display: shown, version, deadline: null });
      void enqueue(version, patch, () => finish(patch, shown));
    } else {
      setCommitted(null);
      wrote.current = false;
    }
  };
  const saveNow = (extra: Dict) => {
    const current = active.current;
    if (!current) return Promise.resolve(false);
    const shown = {
      ...(visible.current || latest.current.presentation),
      ...current.display,
      ...extra,
    };
    const patch = serviceThemeSnapshot(display(shown)),
      version = ++generation.current;
    update({ ...current, version, deadline: null });
    return enqueue(version, patch, () => finish(patch, display(patch)));
  };
  return {
    values: trial?.display || committed?.display || {},
    pending: !!trial,
    saving,
    failed,
    deadline: trial?.deadline || null,
    shuffle,
    restore,
    cancel,
    register,
    saveNow,
  };
}
