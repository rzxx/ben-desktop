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
  const setCenterFilter = useNotificationsStore((state) => state.setCenterFilter);
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
        className={`fixed inset-0 z-40 bg-black/35 backdrop-blur-[2px] transition ${
          centerOpen ? "pointer-events-auto opacity-100" : "pointer-events-none opacity-0"
        }`}
        onClick={() => {
          setCenterOpen(false);
        }}
      />

      <aside
        className={`fixed top-8 right-0 bottom-0 z-50 w-full max-w-[26rem] border-l border-white/8 bg-[linear-gradient(180deg,rgba(19,19,19,0.98),rgba(10,10,10,0.96))] shadow-2xl shadow-black/45 transition-transform duration-300 ${
          centerOpen ? "translate-x-0" : "translate-x-full"
        }`}
      >
        <div className="flex h-full min-h-0 flex-col">
          <header className="flex items-start justify-between gap-3 border-b border-white/8 px-5 py-4">
            <div>
              <p className="text-[0.68rem] tracking-[0.3em] text-white/35 uppercase">
                Notifications
              </p>
              <h2 className="mt-2 text-lg font-semibold text-white">
                Core activity center
              </h2>
              <p className="mt-2 text-sm text-white/48">
                Everything the desktop core is doing, with toast suppression applied only at the shell level.
              </p>
            </div>
            <button
              className="inline-flex h-9 w-9 items-center justify-center rounded-full border border-white/10 bg-white/5 text-white/60 transition hover:border-white/18 hover:text-white"
              onClick={() => {
                setCenterOpen(false);
              }}
              type="button"
            >
              <X className="h-4 w-4" />
            </button>
          </header>

          <div className="flex flex-wrap gap-2 border-b border-white/8 px-5 py-3">
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

          <div className="border-b border-white/8 px-5 py-4">
            <p className="text-[0.68rem] tracking-[0.26em] text-white/35 uppercase">
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
        <h3 className="text-sm font-semibold text-white">{title}</h3>
        <BellRing className="h-4 w-4 text-white/28" />
      </div>

      {items.length === 0 ? (
        <div className="rounded-[1.2rem] border border-dashed border-white/10 bg-white/[0.03] px-4 py-5 text-sm text-white/42">
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
                <div className="pointer-events-none absolute top-3 right-3 rounded-full border border-white/10 bg-black/40 px-2 py-1 text-[0.58rem] tracking-[0.16em] text-white/52 uppercase">
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
          ? "border-sky-400/25 bg-sky-400/15 text-sky-100"
          : "border-white/10 bg-white/5 text-white/58 hover:border-white/18 hover:text-white"
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
          ? "border-emerald-400/25 bg-emerald-400/12 text-white"
          : "border-white/10 bg-white/5 text-white/58 hover:border-white/18 hover:text-white"
      }`}
      onClick={onClick}
      type="button"
    >
      <div className="text-[0.62rem] tracking-[0.18em] uppercase">{label}</div>
    </button>
  );
}
