import { queueFlashToast, type ToastMessage } from "@/components/flash-toast";
import { userStoragePrefix } from "@/features/auth/storage";

export function refreshReviewQueue(userId: string | null | undefined, toast: ToastMessage) {
  if (userId) {
    const prefix = `${userStoragePrefix(userId)}reviews:`;
    const keys = Array.from({ length: sessionStorage.length }, (_, index) => sessionStorage.key(index));
    for (const key of keys) {
      if (key?.startsWith(prefix)) { sessionStorage.removeItem(key); }
    }
  }
  queueFlashToast(toast);
  // Reload also clears expanded client pages; a server refresh alone retains their local state.
  window.location.reload();
}
