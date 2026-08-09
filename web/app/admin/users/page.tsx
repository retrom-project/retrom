import { scalarSearchParams, withQuery } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";
import { UserAdmin, type LinkPage, type UserPage } from "@/features/users/user-admin";

export default async function AdminUsersPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "role", "status", "sort"]);
  const [users, invitations] = await Promise.all([
    backendJSON<UserPage>(withQuery("/api/v1/admin/users", { ...values, limit: "50" })),
    backendJSON<LinkPage>("/api/v1/admin/invitations?state=ACTIVE&limit=50")
  ]);
  const key = new URLSearchParams(values).toString();
  return <UserAdmin key={key} initialUsers={users} initialInvitations={invitations} filterValues={values} />;
}
