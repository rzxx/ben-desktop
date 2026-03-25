import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";

type UIStore = {
  isQueueSidebarOpen: boolean;
  openQueueSidebar: () => void;
  closeQueueSidebar: () => void;
  toggleQueueSidebar: () => void;
};

export const useUIStore = create<UIStore>()(
  persist(
    (set) => ({
      isQueueSidebarOpen: true,
      openQueueSidebar: () => {
        set({ isQueueSidebarOpen: true });
      },
      closeQueueSidebar: () => {
        set({ isQueueSidebarOpen: false });
      },
      toggleQueueSidebar: () => {
        set((state) => ({ isQueueSidebarOpen: !state.isQueueSidebarOpen }));
      },
    }),
    {
      name: "ben.desktop.ui",
      storage: createJSONStorage(() => window.localStorage),
      partialize: (state) => ({
        isQueueSidebarOpen: state.isQueueSidebarOpen,
      }),
    },
  ),
);
