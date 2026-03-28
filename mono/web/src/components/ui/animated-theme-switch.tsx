// ─── Animated Theme Switch ───────────────────────────────────────────────────
// Framer Motion + Radix animated dark/light mode toggle.
// Spring physics, icon rotation, hover glow — production-grade feel.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useEffect, useState } from "react";
import * as SwitchPrimitives from "@radix-ui/react-switch";
import { motion } from "framer-motion";
import { Sun, Moon } from "lucide-react";

export function AnimatedThemeSwitch() {
  const [isDark, setIsDark] = useState(false);

  useEffect(() => {
    setIsDark(document.documentElement.classList.contains("dark"));
  }, []);

  const toggle = (checked: boolean) => {
    setIsDark(checked);
    document.documentElement.classList.toggle("dark", checked);
    localStorage.setItem("theme", checked ? "dark" : "light");
  };

  return (
    <SwitchPrimitives.Root
      checked={isDark}
      onCheckedChange={toggle}
      className="group relative inline-flex h-8 w-14 shrink-0 cursor-pointer items-center rounded-full border-2 border-transparent bg-zinc-200 transition-colors duration-300 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background data-[state=checked]:bg-indigo-600 dark:bg-zinc-700 dark:data-[state=checked]:bg-indigo-500"
    >
      {/* Subtle glow on hover */}
      <span className="pointer-events-none absolute inset-0 rounded-full opacity-0 transition-opacity group-hover:opacity-100 group-hover:shadow-[0_0_8px_2px_rgba(99,102,241,0.25)] dark:group-hover:shadow-[0_0_8px_2px_rgba(129,140,248,0.3)]" />

      <SwitchPrimitives.Thumb asChild>
        <motion.span
          layout
          transition={{ type: "spring", stiffness: 500, damping: 30 }}
          className="pointer-events-none flex h-6 w-6 items-center justify-center rounded-full bg-white shadow-lg data-[state=unchecked]:translate-x-0.5 data-[state=checked]:translate-x-[1.625rem]"
        >
          <motion.span
            animate={{ rotate: isDark ? 360 : 0 }}
            transition={{ duration: 0.5, ease: "easeInOut" }}
            className="flex items-center justify-center"
          >
            {isDark ? (
              <Moon className="h-3.5 w-3.5 text-indigo-600" />
            ) : (
              <Sun className="h-3.5 w-3.5 text-amber-500" />
            )}
          </motion.span>
        </motion.span>
      </SwitchPrimitives.Thumb>
    </SwitchPrimitives.Root>
  );
}
