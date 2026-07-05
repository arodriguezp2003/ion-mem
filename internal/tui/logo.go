package tui

// logoRows is the compact ASCII art for "ION MEM" rendered in a clean,
// readable style. 7 rows — balanced between impact and space budget.
// Source: figlet colossal font, hand-trimmed for TUI use.
var logoRows = [7]string{
	`  ██╗ ██████╗ ███╗   ██╗    ███╗   ███╗███████╗███╗   ███╗`,
	`  ██║██╔═══██╗████╗  ██║    ████╗ ████║██╔════╝████╗ ████║`,
	`  ██║██║   ██║██╔██╗ ██║    ██╔████╔██║█████╗  ██╔████╔██║`,
	`  ██║██║   ██║██║╚██╗██║    ██║╚██╔╝██║██╔══╝  ██║╚██╔╝██║`,
	`  ██║╚██████╔╝██║ ╚████║    ██║ ╚═╝ ██║███████╗██║ ╚═╝ ██║`,
	`  ╚═╝ ╚═════╝ ╚═╝  ╚═══╝    ╚═╝     ╚═╝╚══════╝╚═╝     ╚═╝`,
	``,
}

// logoHeight is the number of rows the logo occupies when rendered, including
// the tagline line and the blank spacer below it.
// 7 art rows + 1 tagline line + 1 blank spacer = 9 total lines consumed in the content area.
const logoHeight = 9

// logoMinTermHeight is the minimum terminal height at which the logo is shown.
// Below this threshold the compact one-line brand header in the chrome bar is
// sufficient and the list must not sacrifice space.
const logoMinTermHeight = 24

// logoGradient lists 6 foreground colors in dark-to-light progression within
// the accent purple family. Applied per-row to create a vertical gradient.
var logoGradient = [6]string{
	"#4B2D9F", // darkest — base purple
	"#5B35C0",
	"#6A42D6",
	"#7D56F4", // accent (matches defaultTheme.accent Dark)
	"#9B7CF8",
	"#BBA8FC", // lightest — near-white violet
}
