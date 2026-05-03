"use client";

import * as Collapsible from "@radix-ui/react-collapsible";
import { cn } from "@utils/helpers";
import classNames from "classnames";
import { ChevronDownIcon, ChevronUpIcon, DotIcon } from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import React, { useMemo } from "react";
import { useApplicationContext } from "@/contexts/ApplicationProvider";

export type SidebarItemProps = {
  onClick?: () => void;
  icon?: React.ReactNode;
  children?: React.ReactNode;
  label?: React.ReactNode;
  collapsible?: boolean;
  className?: string;
  isChild?: boolean;
  href?: string;
  exactPathMatch?: boolean;
  target?: string;
  labelClassName?: string;
  visible: boolean;
};

export default function SidebarItem({
  icon,
  children,
  label,
  collapsible = false,
  className,
  isChild = false,
  href = "",
  exactPathMatch = false,
  target = "_self",
  labelClassName,
  visible,
}: Readonly<SidebarItemProps>) {
  const path = usePathname();
  const router = useRouter();
  const { mobileNavOpen, toggleMobileNav, isNavigationCollapsed } =
    useApplicationContext();

  // For collapsible parents, walk the children and see if any of
  // their `href` props matches the current path. Used to auto-open
  // the section on first render (so deep-linking to /team/users
  // shows the Team submenu open with Users highlighted) and to keep
  // it open when navigation flips between sibling submenus.
  const hasActiveChild = useMemo(() => {
    if (!collapsible || !children) return false;
    let found = false;
    React.Children.forEach(children, (child) => {
      if (!React.isValidElement(child)) return;
      const childHref = (child.props as { href?: string })?.href;
      const childExact = (child.props as { exactPathMatch?: boolean })
        ?.exactPathMatch;
      if (!childHref) return;
      const matches = childExact
        ? path === childHref
        : path.includes(childHref);
      if (matches) found = true;
    });
    return found;
  }, [collapsible, children, path]);

  const [open, setOpen] = React.useState<boolean>(hasActiveChild);

  // Force-open when navigation activates a child (the user can still
  // close manually afterwards — only the rising edge of
  // hasActiveChild reopens).
  React.useEffect(() => {
    if (hasActiveChild) setOpen(true);
  }, [hasActiveChild]);

  const handleClick = () => {
    const preventRedirect = href
      ? exactPathMatch
        ? path == href
        : path.includes(href)
      : false;
    if (collapsible && mobileNavOpen) return;
    if (collapsible && open) return;
    if (preventRedirect) return;
    if (target == "_blank") return window.open(href, "_blank");
    if (mobileNavOpen) toggleMobileNav();
    router.push(href);
  };

  const isActive = useMemo(() => {
    if (collapsible) return false;
    return href ? (exactPathMatch ? path == href : path.includes(href)) : false;
  }, [path, href, exactPathMatch, collapsible]);

  if (!visible) return;

  return (
    <Collapsible.Root open={open} onOpenChange={setOpen}>
      <Collapsible.Trigger asChild>
        <li className={"px-4 cursor-pointer list-none"}>
          <button
            className={classNames(
              "rounded-lg text-[.87rem] w-full relative font-normal",
              className,
              isChild
                ? "pl-7 pr-2 py-[.45rem] mt-1 mb-0.5"
                : "py-[.45rem] px-3",
              isActive
                ? // Active: brand-tinted chip in light so the
                  // selected page is unmistakably distinct from a
                  // mere hover (upstream used `bg-gray-200` for
                  // both, so the highlight got lost in light).
                  // Dark keeps its existing nb-gray-900 surface.
                  "text-openzro-700 bg-openzro-50 dark:text-white dark:bg-nb-gray-900"
                : "text-gray-600 hover:bg-neutral-100 hover:text-gray-900 dark:text-nb-gray-400 dark:hover:bg-nb-gray-900/50",
            )}
            onClick={handleClick}
          >
            {isChild && isNavigationCollapsed && !mobileNavOpen && (
              <div
                className={
                  "absolute left-0 top-0 w-full h-full flex items-center justify-center group-hover/navigation:hidden text-[10px]"
                }
              >
                <DotIcon size={14} className={"shrink-0"} />
              </div>
            )}
            <div
              className={classNames(
                "flex w-full items-center shrink-0 ",
                href == "" ? "disabled pointer-events-none" : "",
              )}
            >
              <span className="peer/icon" data-active={isActive} />
              {icon}

              <span
                className={cn(
                  "px-4 whitespace-nowrap flex-1 w-full text-left",
                  labelClassName,
                  isNavigationCollapsed &&
                    !mobileNavOpen &&
                    "opacity-0 group-hover/navigation:opacity-100",
                )}
              >
                {label}
              </span>
              {collapsible &&
                (open ? (
                  <ChevronUpIcon
                    size={18}
                    className={cn(
                      "shrink-0",
                      isNavigationCollapsed &&
                        !mobileNavOpen &&
                        "opacity-0 group-hover/navigation:opacity-100",
                    )}
                  />
                ) : (
                  <ChevronDownIcon
                    size={18}
                    className={cn(
                      "shrink-0",
                      isNavigationCollapsed &&
                        !mobileNavOpen &&
                        "opacity-0 group-hover/navigation:opacity-100",
                    )}
                  />
                ))}
            </div>
          </button>
        </li>
      </Collapsible.Trigger>
      {collapsible && <Collapsible.Content>{children}</Collapsible.Content>}
    </Collapsible.Root>
  );
}
