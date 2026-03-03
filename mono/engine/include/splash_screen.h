#pragma once
/**
 * RTVortex Splash Screen
 *
 * Platform behavior:
 *   - Windows:  Native Win32 borderless popup showing rv_splash.jpg
 *   - macOS:    Native NSWindow popup showing rv_splash.jpg (future)
 *   - Linux:    No splash (headless). Terminal banner printed separately.
 *
 * The splash runs on a background thread and auto-dismisses when
 * dismiss() is called (i.e., after the engine finishes initialization).
 *
 * Users can disable the splash with the -noSplashScreen flag.
 */

#include <string>
#include <thread>
#include <atomic>
#include <filesystem>

#ifdef _WIN32
#include <windows.h>
#include <objidl.h>
#include <gdiplus.h>
#pragma comment(lib, "gdiplus.lib")
#elif defined(__APPLE__)
// macOS splash - future implementation
#endif

namespace rtvortex {

class SplashScreen {
public:
    /**
     * Show the splash screen (Windows/macOS only).
     * On Linux this is a no-op — Linux is headless, use the terminal banner.
     *
     * @param image_path  Path to rv_splash.jpg
     */
    static void show(const std::string& image_path) {
#if defined(_WIN32)
        if (!std::filesystem::exists(image_path)) return;
        s_dismissed.store(false);
        s_splash_thread = std::thread([image_path]() {
            showWindowsSplash(image_path);
        });
#elif defined(__APPLE__)
        // macOS implementation placeholder
        (void)image_path;
#else
        // Linux: no-op (headless)
        (void)image_path;
#endif
    }

    /**
     * Dismiss the splash screen.
     * Called once the engine is fully initialized and listening.
     * Safe to call multiple times or if splash was never shown.
     */
    static void dismiss() {
        s_dismissed.store(true);
#if defined(_WIN32) || defined(__APPLE__)
        if (s_splash_thread.joinable()) {
            s_splash_thread.join();
        }
#endif
    }

    /**
     * Check if splash is currently active.
     */
    static bool isActive() {
        return !s_dismissed.load();
    }

private:
    static inline std::atomic<bool> s_dismissed{true};
    static inline std::thread s_splash_thread;

#ifdef _WIN32
    static inline HWND s_hwnd = nullptr;
    static inline ULONG_PTR s_gdiplus_token = 0;

    static LRESULT CALLBACK SplashWndProc(HWND hwnd, UINT msg,
                                           WPARAM wParam, LPARAM lParam)
    {
        switch (msg) {
        case WM_PAINT: {
            PAINTSTRUCT ps;
            HDC hdc = BeginPaint(hwnd, &ps);

            // Load and draw the splash image
            auto* image_path = reinterpret_cast<const wchar_t*>(
                GetWindowLongPtrW(hwnd, GWLP_USERDATA));
            if (image_path) {
                Gdiplus::Graphics graphics(hdc);
                Gdiplus::Image image(image_path);
                if (image.GetLastStatus() == Gdiplus::Ok) {
                    RECT rc;
                    GetClientRect(hwnd, &rc);
                    graphics.DrawImage(&image, 0, 0,
                                        rc.right - rc.left,
                                        rc.bottom - rc.top);
                }
            }

            EndPaint(hwnd, &ps);
            return 0;
        }
        case WM_DESTROY:
            PostQuitMessage(0);
            return 0;
        }
        return DefWindowProcW(hwnd, msg, wParam, lParam);
    }

    static void showWindowsSplash(const std::string& image_path) {
        // Initialize GDI+ for JPEG rendering
        Gdiplus::GdiplusStartupInput gdiplusStartupInput;
        Gdiplus::GdiplusStartup(&s_gdiplus_token, &gdiplusStartupInput, NULL);

        // Convert path to wide string
        std::wstring wide_path(image_path.begin(), image_path.end());

        // Load image to get dimensions
        Gdiplus::Image image(wide_path.c_str());
        int splash_w = 640;
        int splash_h = 400;
        if (image.GetLastStatus() == Gdiplus::Ok) {
            splash_w = static_cast<int>(image.GetWidth());
            splash_h = static_cast<int>(image.GetHeight());
            // Cap at reasonable size
            if (splash_w > 800) { splash_h = splash_h * 800 / splash_w; splash_w = 800; }
            if (splash_h > 600) { splash_w = splash_w * 600 / splash_h; splash_h = 600; }
        }

        // Register window class
        const wchar_t CLASS_NAME[] = L"RTVortexSplash";
        WNDCLASSW wc = {};
        wc.lpfnWndProc   = SplashWndProc;
        wc.hInstance      = GetModuleHandle(NULL);
        wc.lpszClassName  = CLASS_NAME;
        wc.hCursor        = LoadCursor(NULL, IDC_ARROW);
        RegisterClassW(&wc);

        // Center on screen
        int screen_w = GetSystemMetrics(SM_CXSCREEN);
        int screen_h = GetSystemMetrics(SM_CYSCREEN);
        int x = (screen_w - splash_w) / 2;
        int y = (screen_h - splash_h) / 2;

        // Create borderless popup
        s_hwnd = CreateWindowExW(
            WS_EX_TOPMOST | WS_EX_TOOLWINDOW,
            CLASS_NAME,
            L"RTVortex",
            WS_POPUP | WS_VISIBLE,
            x, y, splash_w, splash_h,
            NULL, NULL, GetModuleHandle(NULL), NULL
        );

        if (!s_hwnd) {
            Gdiplus::GdiplusShutdown(s_gdiplus_token);
            return;
        }

        // Store image path for WM_PAINT
        wchar_t* persistent_path = new wchar_t[wide_path.size() + 1];
        wcscpy(persistent_path, wide_path.c_str());
        SetWindowLongPtrW(s_hwnd, GWLP_USERDATA,
                          reinterpret_cast<LONG_PTR>(persistent_path));

        ShowWindow(s_hwnd, SW_SHOW);
        UpdateWindow(s_hwnd);

        // Message loop — runs until dismiss() is called
        while (!s_dismissed.load()) {
            MSG msg;
            while (PeekMessage(&msg, NULL, 0, 0, PM_REMOVE)) {
                if (msg.message == WM_QUIT) {
                    s_dismissed.store(true);
                    break;
                }
                TranslateMessage(&msg);
                DispatchMessage(&msg);
            }
            Sleep(30);
        }

        // Cleanup
        auto* ptr = reinterpret_cast<wchar_t*>(
            GetWindowLongPtrW(s_hwnd, GWLP_USERDATA));
        delete[] ptr;

        DestroyWindow(s_hwnd);
        UnregisterClassW(CLASS_NAME, GetModuleHandle(NULL));
        Gdiplus::GdiplusShutdown(s_gdiplus_token);
        s_hwnd = nullptr;
    }
#endif  // _WIN32
};

}  // namespace rtvortex
