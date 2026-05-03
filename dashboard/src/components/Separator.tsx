export default function Separator() {
  // 1px hairline. Light: neutral-200 (#E5E7EB) reads as a clean
  // divider on white surfaces. Dark: keep the original zinc-700/40
  // ink-violet line.
  return (
    <span
      className={"h-[1px] w-full bg-neutral-200 dark:bg-zinc-700/40 block"}
    ></span>
  );
}
