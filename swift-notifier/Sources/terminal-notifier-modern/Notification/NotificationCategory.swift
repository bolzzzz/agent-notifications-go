import Foundation
import UserNotifications

enum NotificationCategory {

    static let categoryIdentifier = "AGENT_NOTIFICATION"

    static func register() {
        let openAction = UNNotificationAction(
            identifier: "OPEN",
            title: "Open",
            options: [.foreground]
        )

        let dismissAction = UNNotificationAction(
            identifier: "DISMISS",
            title: "Dismiss",
            options: [.destructive]
        )

        let category = UNNotificationCategory(
            identifier: categoryIdentifier,
            actions: [openAction, dismissAction],
            intentIdentifiers: [],
            options: []
        )

        UNUserNotificationCenter.current().setNotificationCategories([category])
    }
}
