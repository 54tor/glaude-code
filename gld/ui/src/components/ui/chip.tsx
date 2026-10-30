import * as React from "react"
import { cn } from "cn"
import { Slot } from "radix-ui"
import { xsBox } from "./xs"

// Information non cliquable à la taille d'un bouton xs : cadre, hauteur et rayon identiques, pour s'aligner avec eux.
function Chip({ className, asChild = false, ...props }: React.ComponentProps<"span"> & { asChild?: boolean }) {
  const Comp = asChild ? Slot.Root : "span"
  return <Comp data-slot="chip" className={cn("inline-flex shrink-0 items-center border", xsBox, className)} {...props} />
}

export { Chip }
