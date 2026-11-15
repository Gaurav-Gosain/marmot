/* The driver's MARMOT_NOPTR=1 linker script (internal/driver/noptr.go), for
   gc_noptr.c: package variables without pointers between the symbols the
   runtime skips. */
SECTIONS
{
  .marmot_noptrdata : ALIGN(64)
  {
    marmot_noptrdata_start = .;
    *(.data.marmot.noptr.*)
    marmot_noptrdata_end = .;
  }
}
INSERT BEFORE .data;
SECTIONS
{
  .marmot_noptrbss (NOLOAD) : ALIGN(64)
  {
    marmot_noptrbss_start = .;
    *(.bss.marmot.noptr.*)
    marmot_noptrbss_end = .;
  }
}
INSERT BEFORE .bss;
