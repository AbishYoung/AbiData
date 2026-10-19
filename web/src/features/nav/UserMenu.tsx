import { DropdownMenu } from "radix-ui";
import { useNavigate } from "@tanstack/react-router";
import {
  AtSign,
  Check,
  ChevronDown,
  CircleHelp,
  KeyRound,
  LogOut,
  Monitor,
  Moon,
  PenLine,
  Rows3,
  Rows4,
  Settings,
  Shield,
  Sun,
} from "lucide-react";
import { queryClient } from "@/app/queryClient";
import { useLogout, type Me } from "@/lib/session";
import { isThemePref, useThemeStore } from "@/lib/theme";
import { useDensityStore } from "@/lib/density";
import { useIsCompact } from "@/lib/useMediaQuery";
import { openDocs } from "@/features/docs/docsStore";
import { openChangePassword } from "@/features/account/ChangePasswordDialog";
import { openChangeUsername } from "@/features/account/ChangeUsernameDialog";
import { btn, menu, menuItem, menuSeparator } from "@/components/styles";

const item = menuItem;
const THEME_ICON = { system: Monitor, light: Sun, dark: Moon } as const;
const DENSITIES = [
  { value: "compact", label: "Compact rows", Icon: Rows4 },
  { value: "comfortable", label: "Comfortable rows", Icon: Rows3 },
] as const;

function initialsOf(name: string): string {
  const parts = name.split(/[\s@.]+/).filter(Boolean);
  return (
    parts
      .slice(0, 2)
      .map((w) => w[0]?.toUpperCase())
      .join("") || "?"
  ).slice(0, 2);
}

export function UserMenu({
  me,
  onWorkspaceSettings,
}: {
  me: Me;
  onWorkspaceSettings?: () => void;
}) {
  const navigate = useNavigate();
  const pref = useThemeStore((s) => s.pref);
  const setPref = useThemeStore((s) => s.setPref);
  const density = useDensityStore((s) => s.density);
  const setDensity = useDensityStore((s) => s.setDensity);
  const logout = useLogout(queryClient, () => void navigate({ to: "/login" }));

  const compact = useIsCompact();
  const who = me.user.display_name || me.user.email;
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger className={`${btn} !pl-1.5 max-sm:!px-1.5`} aria-label={who}>
        <span
          aria-hidden="true"
          className="inline-flex h-6 w-6 items-center justify-center rounded-full bg-accent-soft text-xs font-semibold text-accent"
        >
          {initialsOf(who)}
        </span>
        <span aria-hidden="true" className="hidden max-w-40 truncate sm:inline">
          {who}
        </span>
        <ChevronDown size={14} aria-hidden="true" className="text-fg-subtle max-sm:hidden" />
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content
          align="end"
          sideOffset={4}
          className={`ui-menu z-50 min-w-56 ${menu} text-fg`}
        >
          <div className="px-2 pb-2 pt-1.5">
            <p className="m-0 truncate text-sm font-semibold text-fg">{who}</p>
            {me.user.display_name && (
              <p className="m-0 truncate text-xs text-fg-muted">
                {me.user.email}
              </p>
            )}
            {me.user.username && (
              <p className="m-0 truncate text-xs text-fg-muted">
                @{me.user.username}
              </p>
            )}
          </div>
          <DropdownMenu.Separator className={menuSeparator} />
          {onWorkspaceSettings && (
            <>
              <DropdownMenu.Item
                className={item}
                onSelect={onWorkspaceSettings}
              >
                <Settings size={16} aria-hidden="true" />
                Workspace settings
              </DropdownMenu.Item>
              <DropdownMenu.Separator className={menuSeparator} />
            </>
          )}
          {compact && (
            <>
              <DropdownMenu.Item className={item} onSelect={() => openDocs()}>
                <CircleHelp size={16} aria-hidden="true" />
                Documentation
              </DropdownMenu.Item>
              <DropdownMenu.Separator className={menuSeparator} />
            </>
          )}
          {(me.memberships ?? []).some((m) => m.system_kind === "blog") && (
            <>
              <DropdownMenu.Item className={item} onSelect={() => void navigate({ to: "/blog" })}>
                <PenLine size={16} aria-hidden="true" />
                Blog
              </DropdownMenu.Item>
              <DropdownMenu.Separator className={menuSeparator} />
            </>
          )}
          {me.user.is_platform_admin && (
            <>
              <DropdownMenu.Item
                className={item}
                onSelect={() => void navigate({ to: "/admin" })}
              >
                <Shield size={16} aria-hidden="true" />
                Admin console
              </DropdownMenu.Item>
              <DropdownMenu.Separator className={menuSeparator} />
            </>
          )}
          <DropdownMenu.Label className="px-2 py-1 text-xs font-semibold uppercase tracking-wider text-fg-subtle">
            Theme
          </DropdownMenu.Label>
          <DropdownMenu.RadioGroup
            value={pref}
            onValueChange={(v) => {
              if (isThemePref(v)) setPref(v);
            }}
          >
            {(["system", "light", "dark"] as const).map((t) => (
              <DropdownMenu.RadioItem key={t} value={t} className={item}>
                {(() => {
                  const Icon = THEME_ICON[t];
                  return (
                    <Icon
                      size={16}
                      aria-hidden="true"
                      className="text-fg-muted"
                    />
                  );
                })()}
                <span className="flex-1">
                  {t === "system" ? "System" : t === "light" ? "Light" : "Dark"}
                </span>
                <DropdownMenu.ItemIndicator>
                  <Check size={14} aria-hidden="true" className="text-accent" />
                </DropdownMenu.ItemIndicator>
              </DropdownMenu.RadioItem>
            ))}
          </DropdownMenu.RadioGroup>
          <DropdownMenu.Separator className={menuSeparator} />
          <DropdownMenu.Label className="px-2 py-1 text-xs font-semibold uppercase tracking-wider text-fg-subtle">
            Density
          </DropdownMenu.Label>
          <DropdownMenu.RadioGroup
            aria-label="Row density"
            value={density}
            onValueChange={(v) => {
              const d = DENSITIES.find((o) => o.value === v);
              if (d) setDensity(d.value);
            }}
          >
            {DENSITIES.map(({ value, label, Icon }) => (
              <DropdownMenu.RadioItem key={value} value={value} className={item}>
                <Icon size={16} aria-hidden="true" className="text-fg-muted" />
                <span className="flex-1">{label}</span>
                <DropdownMenu.ItemIndicator>
                  <Check size={14} aria-hidden="true" className="text-accent" />
                </DropdownMenu.ItemIndicator>
              </DropdownMenu.RadioItem>
            ))}
          </DropdownMenu.RadioGroup>
          <DropdownMenu.Separator className={menuSeparator} />
          <DropdownMenu.Item
            className={item}
            // After the menu has closed and returned focus, so the dialog keeps it.
            onSelect={() => setTimeout(openChangePassword, 0)}
          >
            <KeyRound size={16} aria-hidden="true" />
            Change password…
          </DropdownMenu.Item>
          <DropdownMenu.Item className={item} onSelect={() => setTimeout(openChangeUsername, 0)}>
            <AtSign size={16} aria-hidden="true" />
            Change username…
          </DropdownMenu.Item>
          <DropdownMenu.Item className={item} onSelect={() => logout.mutate()}>
            <LogOut size={16} aria-hidden="true" />
            Sign out
          </DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}
