import { globalMetaTitle } from "@utils/meta";
import type { Metadata } from "next";
import BlankLayout from "@/layouts/BlankLayout";

export const metadata: Metadata = {
  title: `Auth Providers - Settings - ${globalMetaTitle}`,
};
export default BlankLayout;
