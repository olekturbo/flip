Feature: Bust mechanics
  Drawing a duplicate number card causes a bust — the player scores 0 for the round.
  Only number cards trigger busts; modifier and action cards never do.

  Scenario: Drawing a duplicate number card causes a bust
    Given Alice has [5, 3]
    And Bob has [7]
    And the deck is [5]
    When Alice draws
    Then Alice is busted

  Scenario: Drawing a new number card is safe
    Given Alice has [5, 3]
    And Bob has [7]
    And the deck is [4]
    When Alice draws
    Then Alice is not busted
    And Alice is active

  Scenario: Busted player scores zero for the round — total score does not increase
    Given Alice has [5, 3]
    And Bob has [7]
    And Alice's total score is 20
    And the deck is [5]
    When Alice draws
    And Bob stops
    Then Alice's total score is 20

  Scenario: Drawing a duplicate modifier card does not cause a bust
    Given Alice has [×2]
    And Bob has [7]
    And the deck is [×2]
    When Alice draws
    Then Alice is not busted
