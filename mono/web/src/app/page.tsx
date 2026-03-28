// Root page — redirect to dashboard.
import { redirect } from "next/navigation";

export default function RootPage() {
  redirect("/dashboard");
}
