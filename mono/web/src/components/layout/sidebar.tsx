// ─── Sidebar ─────────────────────────────────────────────────────────────────
// Collapsible sidebar navigation with links, org switcher placeholder.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  LayoutDashboard,
  GitPullRequest,
  FolderGit2,
  Building2,
  Settings,
  ShieldCheck,
  ChevronLeft,
  ChevronRight,
  Activity,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { useUIStore } from "@/lib/stores/ui";
import { useAuth } from "@/lib/auth/context";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
  TooltipProvider,
} from "@/components/ui/tooltip";

const navItems = [
  { href: "/dashboard", icon: LayoutDashboard, label: "Dashboard" },
  { href: "/dashboard/engine", icon: Activity, label: "Engine Performance" },
  { href: "/reviews", icon: GitPullRequest, label: "Reviews" },
  { href: "/repos", icon: FolderGit2, label: "Repositories" },
  { href: "/orgs", icon: Building2, label: "Organizations" },
  { href: "/settings", icon: Settings, label: "Settings" },
];

const adminItems = [
  { href: "/admin", icon: ShieldCheck, label: "Admin" },
];

export function Sidebar() {
  const pathname = usePathname();
  const { sidebarOpen, toggleSidebar } = useUIStore();
  const { user } = useAuth();

  const isAdmin = user?.role === "admin";

  return (
    <TooltipProvider delayDuration={0}>
      <aside
        className={cn(
          "fixed inset-y-0 left-0 z-30 flex flex-col border-r bg-card transition-all duration-300",
          sidebarOpen ? "w-60" : "w-16",
        )}
      >
        {/* Logo */}
        <div className="flex h-14 items-center border-b px-4">
          <Link
            href="/dashboard"
            className="flex items-center gap-2 font-bold text-primary"
          >
            <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary text-sm font-bold text-primary-foreground">
              V
            </div>
            {sidebarOpen && <span className="text-lg">RTVortex</span>}
          </Link>
        </div>

        {/* Navigation */}
        <nav className="flex-1 space-y-1 px-2 py-4">
          {navItems.map((item) => {
            const isActive =
              pathname === item.href || pathname.startsWith(`${item.href}/`);
            const link = (
              <Link
                key={item.href}
                href={item.href}
                className={cn(
                  "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
                  isActive
                    ? "bg-primary/10 text-primary"
                    : "text-muted-foreground hover:bg-accent hover:text-accent-foreground",
                  !sidebarOpen && "justify-center px-0",
                )}
              >
                <item.icon className="h-5 w-5 shrink-0" />
                {sidebarOpen && <span>{item.label}</span>}
              </Link>
            );

            if (!sidebarOpen) {
              return (
                <Tooltip key={item.href}>
                  <TooltipTrigger asChild>{link}</TooltipTrigger>
                  <TooltipContent side="right">{item.label}</TooltipContent>
                </Tooltip>
              );
            }
            return link;
          })}

          {isAdmin && (
            <>
              <div className="my-2 border-t" />
              {adminItems.map((item) => {
                const isActive =
                  pathname === item.href ||
                  pathname.startsWith(`${item.href}/`);
                const link = (
                  <Link
                    key={item.href}
                    href={item.href}
                    className={cn(
                      "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
                      isActive
                        ? "bg-primary/10 text-primary"
                        : "text-muted-foreground hover:bg-accent hover:text-accent-foreground",
                      !sidebarOpen && "justify-center px-0",
                    )}
                  >
                    <item.icon className="h-5 w-5 shrink-0" />
                    {sidebarOpen && <span>{item.label}</span>}
                  </Link>
                );

                if (!sidebarOpen) {
                  return (
                    <Tooltip key={item.href}>
                      <TooltipTrigger asChild>{link}</TooltipTrigger>
                      <TooltipContent side="right">{item.label}</TooltipContent>
                    </Tooltip>
                  );
                }
                return link;
              })}
            </>
          )}
        </nav>

        {/* Collapse toggle */}
        <div className="border-t p-2">
          <Button
            variant="ghost"
            size="icon"
            className="w-full"
            onClick={toggleSidebar}
            aria-label={sidebarOpen ? "Collapse sidebar" : "Expand sidebar"}
          >
            {sidebarOpen ? (
              <ChevronLeft className="h-4 w-4" />
            ) : (
              <ChevronRight className="h-4 w-4" />
            )}
          </Button>
        </div>
      </aside>
    </TooltipProvider>
  );
}
