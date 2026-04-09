import { BellRing, X } from "lucide-react";
import { NotificationCard } from "@/components/notifications/NotificationCard";
import {
  isNotificationActive,
  matchesNotificationFilter,
  shouldToastNotification,
} from "@/lib/notifications";
import { useNotificationsStore } from "@/stores/notifications/store";

export function NotificationCenter() {
  const centerOpen = useNotificationsStore((state) => state.centerOpen);
  const notifications = useNotificationsStore((state) => state.notifications);
  const preferences = useNotificationsStore((state) => state.preferences);
  const filter = useNotificationsStore((state) => state.centerFilter);
  const setCenterOpen = useNotificationsStore((state) => state.setCenterOpen);
  const setCenterFilter = useNotificationsStore(
    (state) => state.setCenterFilter,
  );
  const setVerbosity = useNotificationsStore((state) => state.setVerbosity);

  const filtered = notifications.filter((notification) =>
    matchesNotificationFilter(notification, filter),
  );
  const active = filtered.filter((notification) =>
    isNotificationActive(notification.phase),
  );
  const recent = filtered.filter(
    (notification) => !isNotificationActive(notification.phase),
  );

  return (
    <>
      <div
        className={`bg-theme-950/12 dark:bg-theme/35 fixed inset-0 z-40 backdrop-blur-[2px] transition ${
          centerOpen
            ? "pointer-events-auto opacity-100"
            : "pointer-events-none opacity-0"
        }`}
        onClick={() => {
          setCenterOpen(false);
        }}
      />

      <aside
        className={`border-theme-300/70 shadow-theme-900/14 dark:bg-theme-950 fixed top-8 right-0 bottom-0 z-50 w-full max-w-[26rem] border-l bg-white/92 shadow-2xl transition-transform duration-300 dark:border-white/8 dark:shadow-black/45 ${
          centerOpen ? "translate-x-0" : "translate-x-full"
        }`}
      >
        <div className="flex h-full min-h-0 flex-col">
          <header className="border-theme-300/70 flex items-start justify-between gap-3 border-b px-5 py-4 dark:border-white/8">
            <div>
              <p className="text-theme-500 text-[0.68rem] tracking-[0.3em] uppercase dark:text-white/35">
                Notifications
              </p>
              <h2 className="text-theme-900 mt-2 text-lg font-semibold dark:text-white">
                Core activity center
              </h2>
              <p className="text-theme-600 mt-2 text-sm dark:text-white/48">
                Everything the desktop core is doing, with toast suppression
                applied only at the shell level.
              </p>
            </div>
            <button
              className="text-theme-600 border-theme-300/75 hover:border-theme-400/75 hover:text-theme-900 inline-flex h-9 w-9 items-center justify-center rounded-full border bg-white/80 transition dark:border-white/10 dark:bg-white/5 dark:text-white/60 dark:hover:border-white/18 dark:hover:text-white"
              onClick={() => {
                setCenterOpen(false);
              }}
              type="button"
            >
              <X className="h-4 w-4" />
            </button>
          </header>

          <div className="border-theme-300/70 flex flex-wrap gap-2 border-b px-5 py-3 dark:border-white/8">
            <FilterPill
              active={filter === "all"}
              label="All"
              onClick={() => setCenterFilter("all")}
            />
            <FilterPill
              active={filter === "user"}
              label="User"
              onClick={() => setCenterFilter("user")}
            />
            <FilterPill
              active={filter === "system"}
              label="System"
              onClick={() => setCenterFilter("system")}
            />
          </div>

          <div className="border-theme-300/70 border-b px-5 py-4 dark:border-white/8">
            <p className="text-theme-500 text-[0.68rem] tracking-[0.26em] uppercase dark:text-white/35">
              Verbosity
            </p>
            <div className="mt-3 grid grid-cols-3 gap-2">
              <VerbosityButton
                active={preferences.verbosity === "important"}
                label="Important"
                onClick={() => void setVerbosity("important")}
              />
              <VerbosityButton
                active={preferences.verbosity === "user_activity"}
                label="User"
                onClick={() => void setVerbosity("user_activity")}
              />
              <VerbosityButton
                active={preferences.verbosity === "everything"}
                label="Everything"
                onClick={() => void setVerbosity("everything")}
              />
            </div>
          </div>

          <div className="ben-scrollbar flex-1 overflow-y-auto px-5 py-4">
            <Section
              empty="No active notifications."
              items={active}
              title={`Active (${active.length})`}
              verbosity={preferences.verbosity}
            />
            <Section
              empty="No recent notifications yet."
              items={recent}
              title={`Recent (${recent.length})`}
              verbosity={preferences.verbosity}
            />
          </div>
        </div>
      </aside>
    </>
  );
}

function Section({
  title,
  items,
  empty,
  verbosity,
}: {
  title: string;
  items: ReturnType<typeof useNotificationsStore.getState>["notifications"];
  empty: string;
  verbosity: string;
}) {
  return (
    <section className="mb-6">
      <div className="mb-3 flex items-center justify-between gap-3">
        <h3 className="text-theme-900 text-sm font-semibold dark:text-white">
          {title}
        </h3>
        <BellRing className="text-theme-400 h-4 w-4 dark:text-white/28" />
      </div>

      {items.length === 0 ? (
        <div className="text-theme-500 border-theme-300/75 rounded-[1.2rem] border border-dashed bg-white/75 px-4 py-5 text-sm dark:border-white/10 dark:bg-white/[0.03] dark:text-white/42">
          {empty}
        </div>
      ) : (
        <div className="space-y-3">
          {items.map((notification) => (
            <div className="relative" key={notification.id}>
              <NotificationCard
                muted={!shouldToastNotification(notification, verbosity)}
                notification={notification}
              />
              {!shouldToastNotification(notification, verbosity) && (
                <div className="text-theme-600 border-theme-300/80 pointer-events-none absolute top-3 right-3 rounded-full border bg-white/88 px-2 py-1 text-[0.58rem] tracking-[0.16em] uppercase dark:border-white/10 dark:bg-black/40 dark:text-white/52">
                  Quieted
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

function FilterPill({
  label,
  active,
  onClick,
}: {
  label: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      className={`inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-xs tracking-[0.18em] uppercase transition ${
        active
          ? "border-sky-400/30 bg-sky-400/12 text-sky-700 dark:text-sky-100"
          : "border-theme-300/75 text-theme-600 hover:border-theme-400/75 hover:text-theme-900 bg-white/75 dark:border-white/10 dark:bg-white/5 dark:text-white/58 dark:hover:border-white/18 dark:hover:text-white"
      }`}
      onClick={onClick}
      type="button"
    >
      {label}
    </button>
  );
}

function VerbosityButton({
  label,
  active,
  onClick,
}: {
  label: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      className={`rounded-xl border px-3 py-3 text-left transition ${
        active
          ? "border-emerald-400/30 bg-emerald-400/12 text-emerald-700 dark:text-white"
          : "border-theme-300/75 text-theme-600 hover:border-theme-400/75 hover:text-theme-900 bg-white/75 dark:border-white/10 dark:bg-white/5 dark:text-white/58 dark:hover:border-white/18 dark:hover:text-white"
      }`}
      onClick={onClick}
      type="button"
    >
      <div className="text-[0.62rem] tracking-[0.18em] uppercase">{label}</div>
    </button>
  );
}
