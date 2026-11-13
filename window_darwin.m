//go:build darwin

#import <Cocoa/Cocoa.h>
#import <QuartzCore/QuartzCore.h>

extern void goTrayAction(int actionCode);
extern void goLockPeek(int peeking);

@interface LuminaTrayHandler : NSObject
- (void)onMenuAction:(id)sender;
@end

@implementation LuminaTrayHandler
- (void)onMenuAction:(id)sender {
    NSMenuItem *item = (NSMenuItem *)sender;
    goTrayAction((int)[item tag]);
}
@end

static NSStatusItem *luminaStatusItem = nil;
static LuminaTrayHandler *luminaTrayHandler = nil;
static NSMenuItem *luminaLockItem = nil;
static NSTimer *luminaPeekTimer = nil;
static int luminaIsLocked = 0;
static int luminaLastPeek = -1;

static NSWindow* findMainAppWindow(void) {
    Class statusBarWinClass = NSClassFromString(@"NSStatusBarWindow");
    for (NSWindow *win in [NSApp windows]) {
        if (statusBarWinClass && [win isKindOfClass:statusBarWinClass]) {
            continue;
        }
        if ([win contentView] != nil) {
            return win;
        }
    }
    return [NSApp mainWindow];
}

static void stopPeekTimer(void) {
    if (luminaPeekTimer != nil) {
        [luminaPeekTimer invalidate];
        luminaPeekTimer = nil;
    }
    if (luminaLastPeek == 1) {
        luminaLastPeek = 0;
        goLockPeek(0);
    }
    luminaLastPeek = -1;
}

static void startPeekTimer(void) {
    if (luminaPeekTimer != nil) return;
    luminaLastPeek = 0;
    luminaPeekTimer = [NSTimer scheduledTimerWithTimeInterval:0.12 repeats:YES block:^(NSTimer * _Nonnull timer) {
        if (!luminaIsLocked) {
            stopPeekTimer();
            return;
        }
        NSWindow *win = findMainAppWindow();
        if (!win) return;
        BOOL optHeld = (([NSEvent modifierFlags] & NSEventModifierFlagOption) != 0);
        int peek = optHeld ? 1 : 0;
        if (peek != luminaLastPeek) {
            luminaLastPeek = peek;
            [win setIgnoresMouseEvents:(peek == 0)];
            goLockPeek(peek);
        }
    }];
}

void luminaConfigureFloatingWindow(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        NSWindow *win = findMainAppWindow();
        if (!win) return;
        [win setLevel:NSFloatingWindowLevel];
        [win setCollectionBehavior:NSWindowCollectionBehaviorCanJoinAllSpaces | NSWindowCollectionBehaviorFullScreenAuxiliary];
        [win setOpaque:NO];
        [win setBackgroundColor:[NSColor clearColor]];

        NSView *cv = [win contentView];
        if (cv) {
            [cv setWantsLayer:YES];
            [[cv layer] setBackgroundColor:[[NSColor clearColor] CGColor]];
            [[cv layer] setCornerRadius:22.0];
            [[cv layer] setMasksToBounds:YES];
        }

        [win setHasShadow:YES];
        [win invalidateShadow];
    });
}

void luminaSetWindowAppearance(int shadowEnabled, float cornerRadius) {
    dispatch_async(dispatch_get_main_queue(), ^{
        NSWindow *win = findMainAppWindow();
        if (!win) return;
        NSView *cv = [win contentView];
        if (cv) {
            [cv setWantsLayer:YES];
            [[cv layer] setCornerRadius:cornerRadius];
            [[cv layer] setMasksToBounds:YES];
        }
        [win setHasShadow:(shadowEnabled != 0)];
        [win invalidateShadow];
    });
}

void luminaSetClickThrough(int enabled) {
    dispatch_async(dispatch_get_main_queue(), ^{
        NSWindow *win = findMainAppWindow();
        if (!win) return;
        luminaIsLocked = (enabled != 0) ? 1 : 0;
        if (luminaIsLocked) {
            [win setIgnoresMouseEvents:YES];
            startPeekTimer();
        } else {
            stopPeekTimer();
            [win setIgnoresMouseEvents:NO];
        }
        if (luminaLockItem) {
            [luminaLockItem setTitle:(luminaIsLocked ? @"Unlock Click-Through" : @"Lock Click-Through")];
        }
    });
}

void luminaSetupMenuBar(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (luminaStatusItem != nil) return;
        luminaTrayHandler = [[LuminaTrayHandler alloc] init];
        luminaStatusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
        if (@available(macOS 11.0, *)) {
            NSImage *img = [NSImage imageWithSystemSymbolName:@"music.note.list" accessibilityDescription:@"Spofly"];
            [img setTemplate:YES];
            [[luminaStatusItem button] setImage:img];
        } else {
            [[luminaStatusItem button] setTitle:@"♫"];
        }

        NSMenu *menu = [[NSMenu alloc] initWithTitle:@"Spofly"];

        NSMenuItem *toggleVis = [[NSMenuItem alloc] initWithTitle:@"Show / Hide Lyrics" action:@selector(onMenuAction:) keyEquivalent:@""];
        [toggleVis setTarget:luminaTrayHandler];
        [toggleVis setTag:1];
        [menu addItem:toggleVis];

        luminaLockItem = [[NSMenuItem alloc] initWithTitle:@"Lock Click-Through" action:@selector(onMenuAction:) keyEquivalent:@""];
        [luminaLockItem setTarget:luminaTrayHandler];
        [luminaLockItem setTag:2];
        [menu addItem:luminaLockItem];

        [menu addItem:[NSMenuItem separatorItem]];

        NSMenuItem *pillItem = [[NSMenuItem alloc] initWithTitle:@"Pill Mode (Subtitle)" action:@selector(onMenuAction:) keyEquivalent:@""];
        [pillItem setTarget:luminaTrayHandler];
        [pillItem setTag:3];
        [menu addItem:pillItem];

        NSMenuItem *cardItem = [[NSMenuItem alloc] initWithTitle:@"Card Mode" action:@selector(onMenuAction:) keyEquivalent:@""];
        [cardItem setTarget:luminaTrayHandler];
        [cardItem setTag:4];
        [menu addItem:cardItem];

        NSMenuItem *fullItem = [[NSMenuItem alloc] initWithTitle:@"Full Lyrics Mode" action:@selector(onMenuAction:) keyEquivalent:@""];
        [fullItem setTarget:luminaTrayHandler];
        [fullItem setTag:5];
        [menu addItem:fullItem];

        [menu addItem:[NSMenuItem separatorItem]];

        NSMenuItem *quitItem = [[NSMenuItem alloc] initWithTitle:@"Quit Spofly" action:@selector(onMenuAction:) keyEquivalent:@"q"];
        [quitItem setTarget:luminaTrayHandler];
        [quitItem setTag:99];
        [menu addItem:quitItem];

        [luminaStatusItem setMenu:menu];
    });
}
