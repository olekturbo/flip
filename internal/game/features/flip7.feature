Feature: Flip 7 bonus
  Collecting 7 unique number cards ends the round immediately for all players.
  The triggering player receives a +15 bonus on top of their normal score.
  All other active players bank whatever they currently hold.

  Scenario: Collecting the 7th unique number card ends the round
    Given Alice has [1, 2, 3, 4, 5, 6]
    And Bob has [8]
    And the deck is [7]
    When Alice draws
    Then the round has ended

  Scenario: Flip 7 winner receives a +15 bonus on top of their score
    Given Alice has [1, 2, 3, 4, 5, 6]
    And Bob has [8]
    And the deck is [7]
    When Alice draws
    Then Alice's total score is 43

  Scenario: Active players bank their current score when Flip 7 triggers
    Given Alice has [1, 2, 3, 4, 5, 6]
    And Bob has [8]
    And the deck is [7]
    When Alice draws
    Then Bob's total score is 8

  Scenario: Stopped players also keep their score when Flip 7 triggers
    Given Alice has [1, 2, 3, 4, 5, 6]
    And Bob has [8]
    And Bob is stopped
    And the deck is [7]
    When Alice draws
    Then Bob's total score is 8

  Scenario: Flip 7 can be triggered during Flip 3
    Given Alice has [1]
    And Bob has [1, 2, 3, 4, 5, 6]
    And the deck is [Flip 3, 7, 9, 11]
    When Alice draws
    And Alice targets Bob
    Then the round has ended
    And Bob triggered Flip 7
