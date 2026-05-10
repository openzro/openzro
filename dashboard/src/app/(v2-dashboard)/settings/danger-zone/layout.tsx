import { globalMetaTitle } from "@utils/meta";
import type { Metadata } from "next";
import BlankLayout from "@/layouts/BlankLayout";

export const metadata: Metadata = {
  title: `Danger Zone - Settings - ${globalMetaTitle}`,
};
export default BlankLayout;
