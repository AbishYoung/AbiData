import { DropdownMenu } from "radix-ui";
import { useNavigate } from "@tanstack/react-router";
import { Check, ChevronDown, LogOut, User } from "lucide-react";
import { queryClient } from "@/app/queryClient";
import { useLogout, type Me } from "@/lib/session";
import { isThemePref, useThemeStore } from "@/lib/theme";
import { btn } from "@/components/styles";

const item =
  "flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm outline-none data-[highlighted]:bg-accent-soft";

export function UserMenu({ me }: { me: Me }) {
  const navigate = useNavigate();
  const pref = useThemeStore((s) => s.pref);
  const setPref = useThemeStore((s) => s.setPref);
  const logout = useLogout(queryClient, () => void navigate({ to: "/login" }));

  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger className={btn}>
        <User size={16} aria-hidden="true" />
        {me.user.display_name || me.user.email}
        <ChevronDown size={14} aria-hidden="true" />
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content
          align="end"
          sideOffset={4}
          className="z-50 min-w-48 rounded-md border border-border bg-surface p-1 text-fg shadow-lg"
        >
          <DropdownMenu.Label className="px-2 py-1 text-xs text-fg-muted">Theme</DropdownMenu.Label>
          <DropdownMenu.RadioGroup
            value={pref}
            onValueChange={(v) => {
              if (isThemePref(v)) setPref(v);
            }}
          >
            {(["system", "light", "dark"] as const).map((t) => (
              <DropdownMenu.RadioItem key={t} value={t} className={item}>
                <span className="inline-flex w-4 justify-center">
                  <DropdownMenu.ItemIndicator>
                    <Check size={14} aria-hidden="true" />
                  </DropdownMenu.ItemIndicator>
                </span>
                {t === "system" ? "System" : t === "light" ? "Light" : "Dark"}
              </DropdownMenu.RadioItem>
            ))}
          </DropdownMenu.RadioGroup>
          <DropdownMenu.Separator className="my-1 h-px bg-border" />
          <DropdownMenu.Item className={item} onSelect={() => logout.mutate()}>
            <LogOut size={16} aria-hidden="true" />
            Sign out
          </DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}
